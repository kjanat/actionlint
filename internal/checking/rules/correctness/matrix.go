package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// MatrixDeclaration describes the matrix rule.
func MatrixDeclaration() rule.Declaration {
	return rule.Declaration{Name: "matrix", Description: "Validate matrix definitions, combinations, and filters", Category: "correctness", Subcategory: "matrix", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate matrix definitions, combinations, and filters.", Help: "Check axis names and values and ensure include/exclude entries match the intended combinations.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#matrix"}
}
