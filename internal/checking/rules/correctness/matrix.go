package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// MatrixRule is the single definition of the matrix rule.
var MatrixRule = &define.Rule{
	Name:        "matrix",
	Description: "Validate matrix definitions, combinations, and filters",
	Category:    "correctness",
	Subcategory: "matrix",
	Recommended: true,
	Help:        "Check axis names and values and ensure include/exclude entries match the intended combinations.",
	Checks: func(c define.Inputs) define.Checks {
		return MatrixChecks(c.Config.RuleSet().DiagnosticLevel(c.Config, "mixed-type-matrix-filters") != "off")
	},
}
