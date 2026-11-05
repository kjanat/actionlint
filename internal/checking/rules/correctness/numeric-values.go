package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// NumericValuesDeclaration describes the numeric-values rule.
func NumericValuesDeclaration() rule.Declaration {
	return rule.Declaration{Name: "numeric-values", Description: "Report numeric values GitHub truncates or that cannot take effect", Category: "correctness", Subcategory: "values", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report numeric values GitHub truncates toward zero or that cannot take effect, such as a fractional or non-positive max-parallel.", Help: "Write the whole number GitHub will use, greater than zero.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#numeric-values", Examples: []rule.Example{rule.WorkflowExample("Write the whole number GitHub uses for max-parallel.",
		"    strategy:\n      max-parallel: 1.5\n      matrix:\n        n: [1, 2]\n", "    strategy:\n      max-parallel: 1\n      matrix:\n        n: [1, 2]\n")}}
}
