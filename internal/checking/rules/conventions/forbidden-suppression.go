package conventions

import (
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
)

// ForbiddenSuppressionDeclaration describes the forbidden-suppression rule.
func ForbiddenSuppressionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "forbidden-suppression", Aliases: []rule.Alias{{Name: "disallow-suppressions", Deprecated: true}}, Description: "Report suppressions forbidden by policy", Category: "conventions", Subcategory: "suppressions", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Report suppressions forbidden by policy.", Help: "Remove the suppression and address its finding, or change the repository suppression policy.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#forbidden-suppression", Scope: rule.SourceConfiguration, Options: func() any { return &config.SuppressionsPolicy{} }}
}
