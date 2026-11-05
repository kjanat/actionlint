package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// CacheOperationDeclaration describes the cache-operation rule.
func CacheOperationDeclaration() rule.Declaration {
	return rule.Declaration{Name: "cache-operation", Description: "Report cache actions blocked by explicit cache access limits", Category: "correctness", Subcategory: "cache", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report cache actions blocked by explicit cache access limits.", Help: "Align cache actions with the effective cache access limits.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#cache-operation"}
}
