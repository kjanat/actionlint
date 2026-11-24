package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/matrixcheck"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var MatrixRule = &define.Rule{
	Name:        "matrix",
	Description: "Validate matrix definitions, combinations, and filters",
	Category:    "correctness",
	Subcategory: "matrix",
	Recommended: true,
	Help:        "Fix the axis names and values, or the include and exclude entries.",
	Checks: func(c define.RuleInputs) define.Checks {
		return MatrixChecks(c.Config.RuleSet().DiagnosticLevel(c.Config, "mixed-type-matrix-filters") != "off")
	},
}

func MatrixChecks(checkMixedTypeFilters bool) define.Checks {
	return define.Checks{
		Job: func(job workflows.BoundJob) []reporting.StepFinding {
			return reporting.FromDiagnostics(matrixcheck.Check(workflows.MatrixResolution(job.Strategy), checkMixedTypeFilters))
		},
	}
}
