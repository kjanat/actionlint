package actionlint

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (c *LocalActionsCache) findRepositoryMetadata(spec string) (*ActionMetadata, bool, error) {
	if c.caseInsensitive && c.base.proj != nil {
		local, ok := caseInsensitiveActionSpec(c.base.proj.RootDir(), spec)
		if !ok {
			return nil, false, nil
		}
		spec = local
	}
	return c.base.FindMetadata(spec)
}

// Use on-disk spelling for cache keys and diagnostics, even on a Linux host.
// Multiple case-equivalent entries cannot identify a unique runner action.
func caseInsensitiveActionSpec(root, spec string) (string, bool) {
	dir := root
	for component := range strings.SplitSeq(strings.TrimPrefix(spec, "./"), "/") {
		if component == "" || component == "." || component == ".." {
			dir = filepath.Join(dir, component)
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", false
		}
		match := ""
		for _, entry := range entries {
			name := entry.Name()
			if name != component && (!asciiPath(name) || !asciiPath(component) || !strings.EqualFold(name, component)) {
				continue
			}
			if match != "" {
				return "", false
			}
			match = name
		}
		if match == "" {
			return "", false
		}
		dir = filepath.Join(dir, match)
	}
	local, err := filepath.Rel(root, dir)
	return "./" + filepath.ToSlash(local), err == nil
}

// Newer overlapping checkouts shadow older placements; unrelated copies survive.
// Immutable links let conditional composite invocations restore the whole state.
type checkoutPlacement struct {
	directory       runDirectory
	previous        *checkoutPlacement
	caseInsensitive bool
	foreign         bool
}

func (placement *checkoutPlacement) relative(local string) (string, bool) {
	local, prefix := path.Clean(local), path.Clean(placement.directory.path)
	if prefix == "." {
		return local, true
	}
	if len(local) < len(prefix) {
		return "", false
	}
	match := local[:len(prefix)] == prefix
	if !match && placement.caseInsensitive && asciiPath(prefix) && asciiPath(local[:len(prefix)]) {
		match = strings.EqualFold(local[:len(prefix)], prefix)
	}
	if !match {
		return "", false
	}
	if len(local) == len(prefix) {
		return ".", true
	}
	return strings.CutPrefix(local[len(prefix):], "/")
}

func (placement *checkoutPlacement) matching(local string) *checkoutPlacement {
	for current := placement; current != nil; current = current.previous {
		if _, matches := current.relative(local); matches {
			return current
		}
	}
	return nil
}

func (placement *checkoutPlacement) retainsOtherCheckout(checkout string) bool {
	checkout = path.Clean(checkout)
	for current := placement; current != nil; current = current.previous {
		prefix := path.Clean(current.directory.path)
		if current.directory.kind == directoryKnown && prefix != checkout && placement.matching(prefix) == current {
			return true
		}
	}
	return false
}

// The workflow-scoped view translates runner workspace paths before consulting
// the shared repository cache. Its placement never leaks between jobs or files.
func (c *LocalActionsCache) currentCheckout() runDirectory {
	if placement := c.checkoutState(); placement != nil {
		return placement.directory
	}
	return runDirectory{}
}

func (c *LocalActionsCache) checkoutState() *checkoutPlacement {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checkout
}

func (c *LocalActionsCache) setCheckout(checkout runDirectory) {
	var placement *checkoutPlacement
	if checkout.kind != directoryUnspecified {
		placement = &checkoutPlacement{directory: checkout}
	}
	c.restoreCheckout(placement)
}

func (c *LocalActionsCache) restoreCheckout(placement *checkoutPlacement) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkout = placement
}

func (c *LocalActionsCache) localSpec(spec string) (string, bool) {
	if !strings.HasPrefix(spec, "./") {
		return "", false
	}
	state := c.checkoutState()
	if state == nil {
		return spec, true
	}
	placement := state.matching(spec)
	if placement == nil {
		return spec, state.directory.kind == directoryUnknown
	}
	checkout := placement.directory
	if placement.foreign {
		return "", false
	}
	if checkout.kind == directoryUnknown {
		// Retain best-effort validation of literal local metadata. Script rules
		// still receive the unknown placement and cannot assume its runtime paths.
		return spec, true
	}
	if checkout.path == "" || checkout.path == "." {
		return spec, true
	}
	if relative, ok := placement.relative(spec); ok {
		return "./" + relative, true
	}
	return "", false
}

func (c *LocalActionsCache) observeCheckout(step *Step) {
	enabled, known := invocationCondition(step.If)
	if known && !enabled {
		return
	}
	if _, parallel := step.Exec.(*ExecParallel); parallel {
		c.setCheckout(runDirectory{kind: directoryUnknown})
		return
	}
	action, ok := step.Exec.(*ExecAction)
	if !ok || action.Uses == nil {
		return
	}
	name, _, versioned := strings.Cut(action.Uses.Value, "@")
	if !versioned || !strings.EqualFold(name, "actions/checkout") {
		return
	}
	if action.InputsExpression != nil {
		c.setCheckout(runDirectory{kind: directoryUnknown})
		return
	}
	checkoutPath, pathKnown := checkoutInput(action, "path")
	checkoutPath, representable := runnerRelativePath(checkoutPath, c.platform)
	if !pathKnown || !representable {
		c.setCheckout(runDirectory{kind: directoryUnknown})
		return
	}
	checkout := runDirectory{kind: directoryKnown, path: checkoutPath}
	if checkout.path != "" {
		checkout.path = path.Clean(checkout.path)
		if checkout.path == ".." || strings.HasPrefix(checkout.path, "../") {
			c.setCheckout(runDirectory{kind: directoryUnknown})
			return
		}
	}
	certain := known && !boolMayBeTrue(step.ContinueOnError) && !boolMayBeTrue(step.Background)
	if !certain {
		checkout.kind = directoryUnknown
	}
	foreign := false
	for _, key := range []string{"repository", "ref", "sparse-checkout", "github-server-url"} {
		value, inputKnown := checkoutInput(action, key)
		if !inputKnown || value != "" {
			checkout.kind = directoryUnknown
		}
		if certain && (key == "repository" || key == "github-server-url") && inputKnown && value != "" {
			foreign = true
		}
	}
	c.restoreCheckout(&checkoutPlacement{directory: checkout, previous: c.checkoutState(), caseInsensitive: c.caseInsensitive, foreign: foreign})
}
