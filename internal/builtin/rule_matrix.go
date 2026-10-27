package builtin

import (
	"errors"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// RuleMatrix adapts canonical matrix diagnostics to the visitor pipeline.
type RuleMatrix struct {
	workflowvisit.Base
	Configuration
	snapshot *documentparse.SourceSnapshot
}

func NewRuleMatrix(snapshot *documentparse.SourceSnapshot) *RuleMatrix {
	return &RuleMatrix{Base: builtinRuleBase("matrix"), snapshot: snapshot}
}

func (r *RuleMatrix) VisitJobPre(job *workflowlang.Job) error {
	if r.snapshot == nil {
		return errors.New("matrix rule requires a source snapshot")
	}
	view, err := r.snapshot.JobView(job)
	if err != nil {
		return err
	}
	for _, diagnostic := range view.Strategy().MatrixResolution().Check(repoconfig.DiagnosticLevel(r.config, "mixed-type-matrix-filters") != "off") {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(diagnostic))
	}
	return nil
}
