package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
)

// UnpinnedReferenceRule is the single definition of the unpinned-reference rule.
var UnpinnedReferenceRule = &define.Rule{Name: "unpinned-reference", Aliases: []rule.Alias{{Name: "require-commit-hash", Deprecated: true}}, Description: "Require full commit SHAs for actions and workflows, and digests for images", Category: "security", Subcategory: "dependencies", Recommended: false, Help: "Pin uses references to a full commit SHA and container images to a digest.", Options: define.Options[lintconfig.CommitHashOptions](),
	Checks: func(c define.Inputs) define.Checks {
		options := new(lintconfig.CommitHashOptions)
		if c.Config != nil {
			// Parsing and ValidateRuleNames reject options that do not load
			// here; without that check they leave no exclusions.
			setting, _ := c.Config.Lint.Rules.Policy.Rule("unpinned-reference")
			if _, err := setting.LoadOptions(options); err != nil {
				options = new(lintconfig.CommitHashOptions)
			}
		}
		return UnpinnedReferenceChecks(options.Excludes)
	},
	Enabled: func(c define.Inputs) bool { return c.Config.RequiresCommitHash() },
}
