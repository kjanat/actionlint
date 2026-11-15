package conventions

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

// JobTimeoutRule is the single definition of the job-timeout rule.
var JobTimeoutRule = &define.Rule{Name: "job-timeout", Aliases: []rule.Alias{{Name: "require-job-timeout", Deprecated: true}}, Description: "Require explicit job timeouts within configured bounds", Category: "conventions", Subcategory: "jobs", Recommended: false, Help: "Set timeout-minutes on the job within the configured bounds.", Options: define.Options[configuration.JobTimeoutPolicy](),
	Checks: func(c define.Inputs) define.Checks {
		policy := c.Config.RequiresJobTimeout()
		options := TimeoutPolicy{Enabled: policy.Enabled()}
		if lower, ok := policy.MinMinutes(); ok {
			options.Min = new(lower)
		}
		if upper, ok := policy.MaxMinutes(); ok {
			options.Max = new(upper)
		}
		return JobTimeoutChecks(options)
	},
	Enabled: func(c define.Inputs) bool { return c.Config.RequiresJobTimeout().Enabled() },
}
