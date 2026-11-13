package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// SyntaxCheckDeclaration describes the syntax-check rule.
func SyntaxCheckDeclaration() rule.Declaration {
	return rule.Declaration{ExecutionFailure: true, Name: "syntax-check", Description: "Validate GitHub Actions workflow syntax", Category: "correctness", Subcategory: "syntax", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate GitHub Actions workflow syntax.", Help: "Check the indicated YAML field and its expected GitHub Actions value type.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#syntax-check", SupportsActions: true}
}
