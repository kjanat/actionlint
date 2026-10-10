package actionlint

import (
	"path/filepath"

	"actionlint.kjanat.dev/internal/workflownames"
)

func workflowRunNamesAvailable(c ruleContext) bool {
	if c.projectRoot == "" || c.workflowNames == nil {
		return false
	}
	path := c.path
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.workingDir, path)
	}
	return filepath.Dir(path) == filepath.Join(absPath(c.projectRoot), ".github", "workflows")
}

type workflowRunNamesRule struct {
	RuleBase
	root  string
	index *workflownames.Index
}

func (r *workflowRunNamesRule) VisitWorkflowPre(w *Workflow) error {
	if r.config.diagnosticLevel(r.Name()) == "off" {
		return nil
	}
	var references []*String
	for _, event := range w.On {
		if hook, ok := event.(*WebhookEvent); ok && hook.Hook.Value == "workflow_run" {
			references = append(references, hook.Workflows...)
		}
	}
	if len(references) == 0 {
		return nil
	}
	names, err := r.index.ForRoot(r.root)
	if err != nil {
		return err
	}
	if !names.Complete {
		return nil
	}
	for _, ref := range references {
		if !ref.ContainsExpression() && names.Missing(ref.Value) {
			r.Errorf(ref.Pos, "workflow_run references workflow pattern %q, but no workflow name matches it in this repository", ref.Value)
		}
	}
	return nil
}
