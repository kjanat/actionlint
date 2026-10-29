package builtin

import (
	"errors"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// RuleMatrix adapts canonical matrix diagnostics to the visitor pipeline.
type RuleMatrix struct {
	workflowvisit.Base
	Configuration
}

func NewRuleMatrix() *RuleMatrix {
	return &RuleMatrix{Base: builtinRuleBase("matrix")}
}

func (r *RuleMatrix) VisitJobPre(view workflowdecl.JobView) error {
	if !view.Present() {
		return errors.New("matrix rule requires a job declaration")
	}
	for _, diagnostic := range view.Strategy().MatrixResolution().Check(repoconfig.DiagnosticLevel(r.config, "mixed-type-matrix-filters") != "off") {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(diagnostic))
	}
	return nil
}
