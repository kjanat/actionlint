package builtin

import (
	"errors"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// ruleMatrix adapts canonical matrix diagnostics to the visitor pipeline.
type ruleMatrix struct {
	workflowvisit.Base
	configuration
}

// NewRuleMatrix reports matrix diagnostics of each job's strategy.
func NewRuleMatrix() workflowvisit.Rule {
	return &ruleMatrix{Base: builtinRuleBase("matrix")}
}

func (r *ruleMatrix) VisitJobPre(view workflowdecl.JobView) error {
	if !view.Present() {
		return errors.New("matrix rule requires a job declaration")
	}
	for _, diagnostic := range view.Strategy().MatrixResolution().Check(repoconfig.DiagnosticLevel(r.config, "mixed-type-matrix-filters") != "off") {
		r.Report(findingmodel.FromDiagnostic(diagnostic))
	}
	return nil
}
