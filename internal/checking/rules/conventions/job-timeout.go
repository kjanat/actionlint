package conventions

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

// JobTimeoutRule is the single definition of the job-timeout rule.
var JobTimeoutRule = &define.Rule{
	Name:        "job-timeout",
	Aliases:     []rule.Alias{{Name: "require-job-timeout", Deprecated: true}},
	Description: "Require explicit job timeouts within configured bounds",
	Category:    "conventions",
	Subcategory: "jobs",
	LegacyGroup: "policy",
	Policy:      "require-job-timeout",
	Help:        "Set timeout-minutes on the job within the configured bounds.",
	Options:     define.Options[configuration.JobTimeoutPolicy](),
	Checks: func(c define.RuleInputs) define.Checks {
		return JobTimeoutChecks(TimeoutPolicyFrom(c.Config.RequiresJobTimeout()))
	},
	Enabled: func(c define.RuleInputs) bool { return c.Config.RequiresJobTimeout().Enabled() },
}

type TimeoutPolicy struct {
	Enabled  bool
	Min, Max *float64
}

// TimeoutPolicyFrom converts the configured policy into the bounds JobTimeoutChecks enforces.
func TimeoutPolicyFrom(policy *configuration.JobTimeoutPolicy) TimeoutPolicy {
	options := TimeoutPolicy{Enabled: policy.Enabled()}
	if lower, ok := policy.MinMinutes(); ok {
		options.Min = new(lower)
	}
	if upper, ok := policy.MaxMinutes(); ok {
		options.Max = new(upper)
	}
	return options
}

// JobTimeoutChecks copies the configured bounds and checks each job against them.
func JobTimeoutChecks(policy TimeoutPolicy) define.Checks {
	if policy.Min != nil {
		policy.Min = new(*policy.Min)
	}
	if policy.Max != nil {
		policy.Max = new(*policy.Max)
	}
	return define.Checks{Job: func(job workflows.BoundJob) []reporting.StepFinding {
		return JobTimeout(job, policy)
	}}
}

func JobTimeout(n workflows.BoundJob, policy TimeoutPolicy) []reporting.StepFinding {
	rule := reporting.Collector{}

	if !policy.Enabled {
		return rule.Findings
	}

	// A job which calls a reusable workflow cannot set "timeout-minutes:".
	// https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations#supported-keywords-for-jobs-that-call-a-reusable-workflow
	if !n.Present() || n.WorkflowCall != nil || n.Job.Steps == nil {
		return rule.Findings
	}

	timeout := n.TimeoutMinutes
	if timeout == nil {
		rule.Errorf(
			n.Pos,
			"%q is not set in job %q. Set an explicit timeout for this job",
			"timeout-minutes",
			n.ID.Text(),
		)
		return rule.Findings
	}

	if timeout.Expression != nil {
		return rule.Findings
	}
	if policy.Min != nil && timeout.Value < *policy.Min {
		rule.Errorf(timeout.Pos, "%q is %v in job %q, below the configured min-minutes of %v",
			"timeout-minutes", timeout.Value, n.ID.Text(), *policy.Min)
	}
	if policy.Max != nil && timeout.Value > *policy.Max {
		rule.Errorf(timeout.Pos, "%q is %v in job %q, above the configured max-minutes of %v",
			"timeout-minutes", timeout.Value, n.ID.Text(), *policy.Max)
	}
	return rule.Findings
}
