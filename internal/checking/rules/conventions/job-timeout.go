package conventions

import (
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
)

// JobTimeoutDeclaration describes the job-timeout rule.
func JobTimeoutDeclaration() rule.Declaration {
	return rule.Declaration{Name: "job-timeout", Aliases: []rule.Alias{{Name: "require-job-timeout", Deprecated: true}}, Description: "Require explicit job timeouts within configured bounds", Category: "conventions", Subcategory: "jobs", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Require explicit job timeouts within configured bounds.", Help: "Set timeout-minutes on the job within the configured bounds.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#job-timeout", Options: func() any { return &config.JobTimeoutPolicy{} }}
}
