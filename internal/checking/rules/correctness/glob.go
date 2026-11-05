package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// GlobDeclaration describes the glob rule.
func GlobDeclaration() rule.Declaration {
	return rule.Declaration{Name: "glob", Description: "Validate branch, tag, and path filter patterns", Category: "correctness", Subcategory: "filters", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate branch, tag, and path filter patterns.", Help: "Correct the pattern syntax and check how positive and negative patterns are ordered.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#glob"}
}
