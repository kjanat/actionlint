package builtin

import (
	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	matrixcheck "actionlint.kjanat.dev/internal/workflow/matrix"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// RuleMatrix adapts matrix diagnostics to the remaining visitor pipeline.
type RuleMatrix struct {
	workflowvisit.Base
	Configuration
}

func NewRuleMatrix() *RuleMatrix { return &RuleMatrix{Base: builtinRuleBase("matrix")} }

func (r *RuleMatrix) VisitJobPre(job *workflowlang.Job) error {
	for _, d := range matrixcheck.Check(job, repoconfig.DiagnosticLevel(r.config, "mixed-type-matrix-filters") != "off") {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
	}
	return nil
}
