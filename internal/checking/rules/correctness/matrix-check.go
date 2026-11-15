package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	matrixcheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/matrix"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// MatrixChecks reports problems in a job's resolved matrix declarations. The
// caller supplies whether the mixed-type filter rule is enabled; callbacks do
// not load configuration or depend on earlier jobs.
func MatrixChecks(checkMixedTypeFilters bool) define.Checks {
	return define.Checks{
		Job: func(job workflowdecl.Job) []reporting.Finding {
			return reporting.FromDiagnostics(matrixcheck.Check(workflowdecl.MatrixResolution(job.Strategy), checkMixedTypeFilters))
		},
	}
}
