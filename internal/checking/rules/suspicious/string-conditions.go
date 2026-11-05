package suspicious

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// StringConditionsDeclaration describes the string-conditions rule.
func StringConditionsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "string-conditions", Description: "Flag string conditions whose truthiness may be misleading", Category: "suspicious", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Flag string conditions whose truthiness may be misleading.", Help: "Use a boolean expression; a nonempty string, including the string false, is truthy.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#string-conditions"}
}
