package builtin

import (
	"path/filepath"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflownames "actionlint.kjanat.dev/v2/internal/workflow/names"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

func workflowRunNamesAvailable(c Context) bool {
	if c.ProjectRoot == "" || c.WorkflowNames == nil {
		return false
	}
	path := c.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.WorkingDir, path)
	}
	return workflownames.SamePath(filepath.Dir(path), filepath.Join(absolutePath(c.ProjectRoot), ".github", "workflows"))
}

type workflowRunNamesRule struct {
	workflowvisit.Base
	Configuration
	root  string
	index *workflownames.Index
}

func (r *workflowRunNamesRule) VisitWorkflowPre(w *workflowlang.Workflow) error {
	if repoconfig.DiagnosticLevel(r.config, r.Name()) == "off" {
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
