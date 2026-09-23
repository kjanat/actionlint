package actionlint

import (
	"path"
	"runtime"
	"strings"
)

// Newer overlapping checkouts shadow older placements; unrelated copies survive.
// Immutable links let conditional composite invocations restore the whole state.
type checkoutPlacement struct {
	directory runDirectory
	previous  *checkoutPlacement
}

func (placement *checkoutPlacement) matching(local string) *checkoutPlacement {
	local = path.Clean(local)
	for current := placement; current != nil; current = current.previous {
		prefix := path.Clean(current.directory.path)
		if prefix == "." || local == prefix || strings.HasPrefix(local, prefix+"/") {
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
	if checkout.kind == directoryUnknown {
		// Retain best-effort validation of literal local metadata. Script rules
		// still receive the unknown placement and cannot assume its runtime paths.
		return spec, true
	}
	if checkout.path == "" || checkout.path == "." {
		return spec, true
	}
	local := path.Clean(spec)
	if local == checkout.path {
		return "./", true
	}
	if relative, ok := strings.CutPrefix(local, checkout.path+"/"); ok {
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
	checkoutPath, representable := runnerDirectoryPath(checkoutPath, c.platform)
	invalidColon := strings.ContainsRune(checkoutPath, ':') && (c.platform != platformKindMacOrLinux || runtime.GOOS == "windows")
	if !pathKnown || !representable || invalidColon || strings.HasPrefix(checkoutPath, "/") || strings.ContainsRune(checkoutPath, '\x00') {
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
	if !known || boolMayBeTrue(step.ContinueOnError) || boolMayBeTrue(step.Background) {
		checkout.kind = directoryUnknown
	}
	for _, key := range []string{"repository", "ref", "github-server-url"} {
		if value, known := checkoutInput(action, key); !known || value != "" {
			checkout.kind = directoryUnknown
		}
	}
	c.restoreCheckout(&checkoutPlacement{directory: checkout, previous: c.checkoutState()})
}
