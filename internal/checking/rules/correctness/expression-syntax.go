package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ExpressionSyntaxDeclaration describes the expression-syntax rule.
func ExpressionSyntaxDeclaration() rule.Declaration {
	return rule.Declaration{ExecutionFailure: true, Name: "expression-syntax", Description: "Parse expression tokens and grammar", Category: "correctness", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Explanation: "Parse expression tokens and grammar.", Help: "Correct the indicated expression token, literal or delimiter.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#expression-syntax", Recommended: true, SupportsActions: true}
}
