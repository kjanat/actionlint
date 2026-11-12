package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ExpressionReferenceDeclaration describes the expression-reference rule.
func ExpressionReferenceDeclaration() rule.Declaration {
	return rule.Declaration{Name: "expression-reference", Description: "Resolve expression variables, properties and functions", Category: "correctness", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Explanation: "Resolve expression variables, properties and functions.", Help: "Use a declared context, property or function; check the suggested spelling and available names.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#expression-reference", Recommended: true, SupportsActions: true}
}
