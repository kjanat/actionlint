package engine

import (
	"path/filepath"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
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
	workflowvisit.Base
	ruleConfiguration
	root  string
	index *workflownames.Index
}

func (r *workflowRunNamesRule) VisitWorkflowPre(w *workflowlang.Workflow) error {
	if diagnosticLevel(r.config, r.Name()) == "off" {
		return nil
	}
	references := workflownames.WorkflowRunFilters(w)
	if len(references) == 0 {
		return nil
	}
	names, err := r.index.ForRoot(r.root)
	if err != nil {
		return err
	}
	for _, d := range names.CheckWorkflowRun(references) {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
	}
	return nil
}
