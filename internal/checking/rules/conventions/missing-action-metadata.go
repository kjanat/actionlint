package conventions

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// MissingActionMetadataDeclaration describes the missing-action-metadata rule.
func MissingActionMetadataDeclaration() rule.Declaration {
	return rule.Declaration{Name: "missing-action-metadata", Aliases: []rule.Alias{{Name: "require-action-metadata", Deprecated: true}}, Description: "Require inspected metadata for remote actions", Category: "conventions", Subcategory: "dependencies", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Report remote action metadata that could not be inspected. Disabled downloads and offline cache misses are recorded in coverage without a lint finding.", Help: "Check access to the referenced repository and its action.yml or action.yaml.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#missing-action-metadata"}
}
