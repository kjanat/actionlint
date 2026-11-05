package suspicious

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// MixedTypeComparisonsDeclaration describes the mixed-type-comparisons rule.
func MixedTypeComparisonsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "mixed-type-comparisons", Description: "Flag condition comparisons that implicitly convert scalar types", Category: "suspicious", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Flag condition comparisons that implicitly convert scalar types.", Help: "Compare values of matching types or convert them explicitly before comparison.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#mixed-type-comparisons"}
}
