package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// InlineSuppressionDeclaration describes the inline-suppression rule.
func InlineSuppressionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "inline-suppression", Description: "Validate inline suppression directives", Category: "correctness", Subcategory: "suppressions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate inline suppression directives.", Help: "Use a known rule name and place the directive on the declaration it is intended to suppress.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#inline-suppression", Scope: rule.SourceConfiguration}
}
