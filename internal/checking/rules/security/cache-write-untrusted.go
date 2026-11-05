package security

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// CacheWriteUntrustedDeclaration describes the cache-write-untrusted rule.
func CacheWriteUntrustedDeclaration() rule.Declaration {
	return rule.Declaration{Name: "cache-write-untrusted", Description: "Report cache write access granted on low-trust triggers", Category: "security", Subcategory: "cache", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report cache write access granted on low-trust triggers.", Help: "Restrict cache writes for low-trust triggers using explicit cache permissions or an event guard.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#cache-write-untrusted"}
}
