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
	return workflownames.SamePath(filepath.Dir(path), filepath.Join(absPath(c.projectRoot), ".github", "workflows"))
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
	var patterns []string
	allKnown, missing := true, false
	for _, ref := range references {
		if ref == nil {
			allKnown = false
			continue
		}
		value := ref.Value
		if literal := literalExpressionValue(value); literal != nil {
			value = *literal
		} else if ref.ContainsExpression() {
			allKnown = false
			continue
		}
		patterns = append(patterns, value)
		if names.Missing(value) {
			missing = true
			r.Errorf(ref.Pos, "workflow_run references workflow pattern %q, but no workflow name matches it in this repository", value)
		}
	}
	if allKnown && !missing && names.ExcludesAll(patterns) {
		r.Error(references[0].Pos, "workflow_run workflow-name filters exclude every matching workflow in this repository")
	}
	return nil
}
