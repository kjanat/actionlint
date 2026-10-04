package actionlint

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (c *LocalActionsCache) findRepositoryMetadata(spec string) (*ActionMetadata, bool, error) {
	if c.caseInsensitive && c.base.proj != nil {
		local, ok := caseInsensitiveRepositoryPath(c.base.proj.RootDir(), spec)
		if !ok {
			return nil, false, nil
		}
		spec = local
	}
	return c.base.FindMetadata(spec)
}

// Use on-disk spelling for cache keys and diagnostics, even on a Linux host.
// Multiple case-equivalent entries cannot identify a unique runner action.
func caseInsensitiveRepositoryPath(root, spec string) (string, bool) {
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
	redirected      bool
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

// A conditional child may replace only some paths. Keep its shadowing ranges,
// but retain the caller's unaffected checkouts when joining the two branches.
func mergeCheckoutBranches(before, after *checkoutPlacement, mayFail bool) *checkoutPlacement {
	if after == before || after == nil {
		return before
	}
	merged := *after
	merged.previous = mergeCheckoutBranches(before, after.previous, mayFail)
	if mayFail || merged.directory.kind != directoryKnown || merged.foreign || !merged.previous.sameSelfCheckout(merged.directory.path) {
		merged.directory.kind = directoryUnknown
		merged.foreign = false
	}
	return &merged
}

func (placement *checkoutPlacement) sameSelfCheckout(destination string) bool {
	prior := placement.matching(destination)
	if prior == nil || prior.directory.kind != directoryKnown || prior.foreign || prior.redirected {
		return false
	}
	relative, matches := prior.relative(destination)
	return matches && relative == "."
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
	if placement.foreign || placement.redirected {
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
	if c.checkoutEnvUnknown || c.persistentEnvUnknown || checkoutEnvironmentUnknown(step.Env, c.platform) {
		// Git may write outside the requested path or execute a wrapper. Literal
		// metadata fallback is not evidence of the resulting repository contents.
		c.restoreCheckout(&checkoutPlacement{directory: runDirectory{kind: directoryUnknown}, redirected: true})
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
	foreign, self := false, true
	for _, key := range []string{"repository", "ref", "sparse-checkout", "github-server-url"} {
		value, inputKnown := checkoutInput(action, key)
		if !inputKnown || value != "" {
			checkout.kind = directoryUnknown
			self = false
		}
		if certain && (key == "repository" || key == "github-server-url" || key == "ref") && inputKnown && value != "" {
			// A specified revision need not contain the analyzed working-tree
			// metadata. No revision identity is available to prove equivalence.
			foreign = true
		}
	}
	previous := c.checkoutState()
	if self && !known && !boolMayBeTrue(step.ContinueOnError) && !boolMayBeTrue(step.Background) && !stepCanRunAfterFailure(step.If) {
		// Skipping or successfully refreshing the same self checkout both
		// preserve its placement. A failed refresh cannot reach ordinary steps.
		if previous.sameSelfCheckout(checkout.path) {
			checkout.kind = directoryKnown
		}
	}
	c.restoreCheckout(&checkoutPlacement{directory: checkout, previous: previous, caseInsensitive: c.caseInsensitive, foreign: foreign})
}
