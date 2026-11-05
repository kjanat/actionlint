package suspicious

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// CaseInsensitiveConditionsDeclaration describes the case-insensitive-conditions rule.
func CaseInsensitiveConditionsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "case-insensitive-conditions", Description: "Flag case-insensitive identity comparisons", Category: "suspicious", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Flag case-insensitive identity comparisons.", Help: "Expression string comparisons ignore case; avoid using them as case-sensitive identity checks.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#case-insensitive-conditions"}
}
