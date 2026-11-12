package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ExpressionDeclaration describes the expression rule.
func ExpressionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "expression", Description: "Validate expression values assigned to workflow fields", Category: "correctness", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate expression values assigned to workflow fields.", Help: "Use the expression value and interpolation form required by this workflow field. Syntax, references, context availability, calls, operand types, payload properties and script injection have separate rules.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#expression", SupportsActions: true}
}
