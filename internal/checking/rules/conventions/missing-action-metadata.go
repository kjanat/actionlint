package conventions

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// MissingActionMetadataRule is the single definition of the missing-action-metadata rule.
var MissingActionMetadataRule = &define.Rule{Name: "missing-action-metadata", Aliases: []rule.Alias{{Name: "require-action-metadata", Deprecated: true}}, Description: "Require inspected metadata for remote actions", Category: "conventions", Subcategory: "dependencies", Recommended: false, Explanation: "Report remote action metadata that could not be inspected. Disabled downloads and offline cache misses are recorded in coverage without a lint finding.", Help: "Check access to the referenced repository and its action.yml or action.yaml.",
	Checks: func(c define.Inputs) define.Checks { return MissingActionMetadataChecks(c.RemoteActions) },
	Enabled: func(c define.Inputs) bool {
		return c.Config != nil && c.Config.Policy.RequireActionMetadata != nil && *c.Config.Policy.RequireActionMetadata
	},
}
