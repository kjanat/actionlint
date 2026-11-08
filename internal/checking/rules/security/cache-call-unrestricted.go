package security

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// CacheCallUnrestrictedDeclaration describes the cache-call-unrestricted rule.
func CacheCallUnrestrictedDeclaration() rule.Declaration {
	return rule.Declaration{Name: "cache-call-unrestricted", Description: "Report reusable workflow calls without explicit cache limits on low-trust triggers", Category: "security", Subcategory: "cache", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report reusable workflow calls reachable from low-trust triggers without an explicit cache mode. The called workflow can request cache writes despite the trigger's read-only default.", Help: "Set cache-mode to read or none on the calling job or workflow.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#cache-call-unrestricted"}
}
