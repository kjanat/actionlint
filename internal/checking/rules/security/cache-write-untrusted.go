package security

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// CacheWriteUntrustedDeclaration describes the cache-write-untrusted rule.
func CacheWriteUntrustedDeclaration() rule.Declaration {
	return rule.Declaration{Name: "cache-write-untrusted", Description: "Report cache write access granted on low-trust triggers", Category: "security", Subcategory: "cache", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report cache write access reachable from low-trust triggers. Untrusted code or input can poison caches consumed by privileged workflows.", Help: "Set cache-mode to read or none, restrict the job with an event guard, or document a reviewed exception with an inline suppression.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#cache-write-untrusted"}
}
