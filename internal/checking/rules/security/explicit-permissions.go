package security

import (
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
)

// ExplicitPermissionsDeclaration describes the explicit-permissions rule.
func ExplicitPermissionsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "explicit-permissions", Aliases: []rule.Alias{{Name: "require-permissions", Deprecated: true}}, Description: "Require explicit token permissions at workflow or job scope", Category: "security", Subcategory: "permissions", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Require explicit token permissions at workflow or job scope.", Help: "Declare explicit permissions at the scope required by repository policy.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#explicit-permissions", Options: func() any { return &config.PermissionsPolicy{} }}
}
