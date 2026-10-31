package checks

import (
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

type TimeoutPolicy struct {
	Enabled  bool
	Min, Max *float64
}

func JobTimeout(n declaration.JobView, policy TimeoutPolicy) []Finding {
	rule := findingCollector{name: "require-job-timeout"}

	if !policy.Enabled {
		return rule.out
	}

	// A job which calls a reusable workflow cannot set "timeout-minutes:".
	// https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations#supported-keywords-for-jobs-that-call-a-reusable-workflow
	if n.WorkflowCall().Present() || !n.StepsPresent() {
		return rule.out
	}

	if !n.TimeoutMinutes().Present() {
		rule.Errorf(
			viewPosition(n.Pos()),
			"%q is not set in job %q. it is required because \"require-job-timeout\" is enabled in the \"policy\" configuration. a job which does not set it is cancelled after the default of 360 minutes",
			"timeout-minutes",
			n.ID().Value(),
		)
		return rule.out
	}

	if n.TimeoutMinutes().Expression().Present() {
		return rule.out
	}
	if policy.Min != nil && n.TimeoutMinutes().Value() < *policy.Min {
		limit := *policy.Min
		rule.Errorf(
			viewPosition(n.TimeoutMinutes().Pos()),
			"%q is %v in job %q. it must not be smaller than %v because \"min-minutes\" of \"require-job-timeout\" is set in the \"policy\" configuration",
			"timeout-minutes", n.TimeoutMinutes().Value(), n.ID().Value(), limit,
		)
	}
	if policy.Max != nil && n.TimeoutMinutes().Value() > *policy.Max {
		limit := *policy.Max
		rule.Errorf(
			viewPosition(n.TimeoutMinutes().Pos()),
			"%q is %v in job %q. it must not be larger than %v because \"max-minutes\" of \"require-job-timeout\" is set in the \"policy\" configuration",
			"timeout-minutes",
			n.TimeoutMinutes().Value(),
			n.ID().Value(),
			limit,
		)
	}
	return rule.out
}
