package actionlint

import (
	"path"
	"strings"
)

// The workflow-scoped view translates runner workspace paths before consulting
// the shared repository cache. Its placement never leaks between jobs or files.
func (c *LocalActionsCache) currentCheckout() runDirectory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checkout
}

func (c *LocalActionsCache) setCheckout(checkout runDirectory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkout = checkout
}

func (c *LocalActionsCache) localSpec(spec string) (string, bool) {
	if !strings.HasPrefix(spec, "./") {
		return "", false
	}
	checkout := c.currentCheckout()
	if checkout.kind == directoryUnknown {
		return "", false
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
	enabled, known := stepCondition(step.If)
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
	c.setCheckout(runDirectory{kind: directoryUnknown})
	if !known || boolMayBeTrue(step.ContinueOnError) || boolMayBeTrue(step.Background) || action.InputsExpression != nil {
		return
	}
	for _, key := range []string{"repository", "ref"} {
		if input := action.Inputs[key]; input != nil && input.Value != nil && input.Value.Value != "" {
			return
		}
	}
	checkout := runDirectory{kind: directoryKnown}
	if input := action.Inputs["path"]; input != nil && input.Value != nil {
		checkout = workingDirectoryValue(input.Value)
		if checkout.kind != directoryKnown || checkout.path != "" && !localRunnerPath(checkout.path) {
			return
		}
		checkout.path = path.Clean(checkout.path)
		if checkout.path == ".." || strings.HasPrefix(checkout.path, "../") {
			return
		}
	}
	c.setCheckout(checkout)
}
