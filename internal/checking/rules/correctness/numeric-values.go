package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// NumericValuesRule is the single definition of the numeric-values rule.
var NumericValuesRule = &define.Rule{Name: "numeric-values", Description: "Report numeric values GitHub truncates or that cannot take effect", Category: "correctness", Subcategory: "values", Recommended: true, Explanation: "Report numeric values GitHub truncates toward zero or that cannot take effect, such as a fractional or non-positive max-parallel.", Help: "Set max-parallel to a positive whole number.", Examples: []rule.Example{rule.WorkflowExample("Write the whole number GitHub uses for max-parallel.",
	"    strategy:\n      max-parallel: 1.5\n      matrix:\n        n: [1, 2]\n", "    strategy:\n      max-parallel: 1\n      matrix:\n        n: [1, 2]\n")},
	Checks: define.Fixed(define.Checks{Job: NumericValues}),
}
