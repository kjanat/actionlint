package workflows

import (
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/model/calls"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// ASTComparisonCache compares AST-derived metadata with captured-file decoding.
type ASTComparisonCache struct {
	*Cache
	WorkingDir string
}

// Legacy AST conversion is exercised only by metadata parity tests.
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
	// Compare canonical identities without changing caller-owned project or
	// source spellings used by readers and diagnostics.
	p = absPath(p)
	r := absPath(c.root)
	return workflowPathToSpec(r, p)
}

// WriteWorkflowCallEvent writes reusable workflow metadata by converting from WorkflowCallEvent AST
// node. The 'wpath' parameter is a path to the workflow file of the AST, which is a relative to the
// project root directory or an absolute path.
// This method does nothing when (1) no project is set, (2) it could not convert the workflow path
// to workflow call spec, (3) some cache for the workflow is already existing.
// This method is thread safe.
func (c *ASTComparisonCache) WriteWorkflowCallEvent(wpath string, event *definition.WorkflowCallEvent) {
	c.WriteWorkflowCallEventFromWorkflow(wpath, event, nil)
}

// WriteWorkflowCallEventFromWorkflow is like WriteWorkflowCallEvent but also records the permissions
// each job of the reusable workflow requires, taken from the workflow AST. The permission check on
// workflow calls needs them. Passing a nil 'w' is equivalent to WriteWorkflowCallEvent. The same
// do-nothing conditions and thread safety apply.
func (c *ASTComparisonCache) WriteWorkflowCallEventFromWorkflow(wpath string, event *definition.WorkflowCallEvent, w *definition.Workflow) {
	// Convert workflow path to workflow call spec
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
