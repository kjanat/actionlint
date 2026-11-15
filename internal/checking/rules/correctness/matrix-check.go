package correctness

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	matrixcheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/matrix"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// MatrixCheck reports problems in a job's resolved matrix declarations. The
// caller supplies whether the mixed-type filter rule is enabled; callbacks do
// not load configuration or depend on earlier jobs.
func MatrixCheck(checkMixedTypeFilters bool) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: MatrixDeclaration(),
		Job: func(job workflowdecl.Job) []reporting.StepFinding {
			return reporting.FromDiagnostics(matrixcheck.Check(workflowdecl.MatrixResolution(job.Strategy), checkMixedTypeFilters))
		},
	}
}
