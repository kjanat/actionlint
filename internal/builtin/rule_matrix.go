package builtin

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	matrixcheck "actionlint.kjanat.dev/v2/internal/workflow/matrix"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// RuleMatrix runs matrix checks during workflow traversal.
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
