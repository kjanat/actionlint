package reusableworkflows

import (
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/model/calls"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type ASTComparisonCache struct {
	*MetadataCache
	WorkingDir string
}

func absPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

func (c *ASTComparisonCache) convWorkflowPathToSpec(p string) (string, bool) {
	if c.root == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(c.WorkingDir, p)
	}
	p = absPath(p)
	r := absPath(c.root)
	return workflowPathToSpec(r, p)
}

func (c *ASTComparisonCache) WriteWorkflowCallEvent(wpath string, event *definition.WorkflowCallEvent) {
	c.WriteWorkflowCallEventFromWorkflow(wpath, event, nil)
}

func (c *ASTComparisonCache) WriteWorkflowCallEventFromWorkflow(wpath string, event *definition.WorkflowCallEvent, w *definition.Workflow) {
	spec, ok := c.convWorkflowPathToSpec(wpath)
	if !ok {
		return
	}
	c.debug("Workflow call spec from workflow path %s: %s", wpath, spec)

	c.mu.RLock()
	_, ok = c.cache[spec]
	c.mu.RUnlock()
	if ok {
		return
	}

	m := calls.MetadataFromWorkflowCallEvent(event, w)

	c.mu.Lock()
	if _, exists := c.cache[spec]; !exists {
		c.cache[spec] = reusableWorkflowMetadataResult{metadata: m}
	}
	c.mu.Unlock()

	c.debug("Workflow call metadata from workflow %s: %v", wpath, m)
}
