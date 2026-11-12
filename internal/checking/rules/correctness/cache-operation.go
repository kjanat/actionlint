package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// CacheOperationDeclaration describes the cache-operation rule.
func CacheOperationDeclaration() rule.Declaration {
	return rule.Declaration{Name: "cache-operation", Description: "Report cache actions blocked by explicit cache access limits", Category: "correctness", Subcategory: "cache", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report cache save or restore operations blocked by an explicit cache mode. GitHub skips the blocked operations.", Help: "Remove the cache step or choose an action and cache mode that allow the intended operation.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#cache-operation"}
}
