package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	matrixcheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/matrix"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

// MatrixRule is the single definition of the matrix rule.
var MatrixRule = &define.Rule{
	Name:        "matrix",
	Description: "Validate matrix definitions, combinations, and filters",
	Category:    "correctness",
	Subcategory: "matrix",
	Recommended: true,
	Help:        "Check axis names and values and ensure include/exclude entries match the intended combinations.",
	Checks: func(c define.RuleInputs) define.Checks {
		return MatrixChecks(c.Config.RuleSet().DiagnosticLevel(c.Config, "mixed-type-matrix-filters") != "off")
	},
}

// MatrixChecks reports problems in a job's resolved matrix declarations. The
// caller supplies whether the mixed-type filter rule is enabled; callbacks do
// not load configuration or depend on earlier jobs.
func MatrixChecks(checkMixedTypeFilters bool) define.Checks {
	return define.Checks{
		Job: func(job workflows.Job) []reporting.StepFinding {
			return reporting.FromDiagnostics(matrixcheck.Check(workflows.MatrixResolution(job.Strategy), checkMixedTypeFilters))
		},
	}
}
