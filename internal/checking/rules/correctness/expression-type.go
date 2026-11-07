package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ExpressionTypeDeclaration describes the expression-type rule.
func ExpressionTypeDeclaration() rule.Declaration {
	return rule.Declaration{Name: "expression-type", Description: "Validate expression operand and argument types", Category: "correctness", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Explanation: "Validate expression operand and argument types.", Help: "Supply the expected value type; convert serialized values explicitly when needed.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#expression-type", Recommended: true, SupportsActions: true}
}
