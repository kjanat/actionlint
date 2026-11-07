package conventions

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// MissingActionMetadataDeclaration describes the missing-action-metadata rule.
func MissingActionMetadataDeclaration() rule.Declaration {
	return rule.Declaration{Name: "missing-action-metadata", Aliases: []rule.Alias{{Name: "require-action-metadata", Deprecated: true}}, Description: "Require inspected metadata for remote actions", Category: "conventions", Subcategory: "dependencies", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Require usable metadata for remote action references without enabling network access.", Help: "Select --action-metadata=online to acquire full SHA-1 manifests, or warm the immutable cache and select offline. Named refs require an action-profile resolver with selection evidence. Invalid parse/schema metadata retains its diagnostics; this does not prove execution or transitive dependency coverage.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#missing-action-metadata"}
}
