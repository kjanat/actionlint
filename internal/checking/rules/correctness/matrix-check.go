package correctness

import (
	"errors"

	matrixcheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/matrix"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ruleMatrix reports the matrix diagnostics attached to each job.
type ruleMatrix struct {
	workflowvisit.Base
	checkMixedTypeFilters bool
}

// NewRuleMatrix reports matrix diagnostics of each job's strategy.
func NewRuleMatrix(checkMixedTypeFilters bool) workflowvisit.Rule {
	descriptor := MatrixDeclaration()
	return &ruleMatrix{Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description), checkMixedTypeFilters: checkMixedTypeFilters}
}

func (r *ruleMatrix) VisitJobPre(view workflowdecl.JobView) error {
	if !view.Present() {
		return errors.New("matrix rule requires a job declaration")
	}
	for _, diagnostic := range matrixcheck.Check(view.Strategy().MatrixResolution(), r.checkMixedTypeFilters) {
		r.Report(findingmodel.FromDiagnostic(diagnostic))
	}
	return nil
}
