package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ExpressionCallDeclaration describes the expression-call rule.
func ExpressionCallDeclaration() rule.Declaration {
	return rule.Declaration{ExecutionFailure: true, Name: "expression-call", Description: "Validate function invocation and constant arguments", Category: "correctness", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Explanation: "Validate function invocation and constant arguments.", Help: "Check parentheses, argument count, format placeholders and literal JSON.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#expression-call", Recommended: true, SupportsActions: true}
}
