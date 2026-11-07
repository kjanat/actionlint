package conventions

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

type TimeoutPolicy struct {
	Enabled  bool
	Min, Max *float64
}

// JobTimeoutCheck binds the configured bounds to job declarations.
// The check owns its bounds, so later caller edits cannot change its policy.
func JobTimeoutCheck(policy TimeoutPolicy) checkinputs.DeclarationCheck {
	if policy.Min != nil {
		policy.Min = new(*policy.Min)
	}
	if policy.Max != nil {
		policy.Max = new(*policy.Max)
	}
	return checkinputs.DeclarationCheck{Declaration: JobTimeoutDeclaration(), Job: func(job declaration.JobView) []reporting.Finding {
		return JobTimeout(job, policy)
	}}
}

func JobTimeout(n declaration.JobView, policy TimeoutPolicy) []reporting.Finding {
	rule := reporting.Collector{Rule: "job-timeout"}

	if !policy.Enabled {
		return rule.Findings
	}

	// A job which calls a reusable workflow cannot set "timeout-minutes:".
	// https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations#supported-keywords-for-jobs-that-call-a-reusable-workflow
	if n.WorkflowCall().Present() || !n.StepsPresent() {
		return rule.Findings
	}

	if !n.TimeoutMinutes().Present() {
		rule.Errorf(
			reporting.ViewPosition(n.Pos()),
			"%q is not set in job %q. it is required because \"job-timeout\" is enabled in the \"policy\" configuration. a job which does not set it is cancelled after the default of 360 minutes",
			"timeout-minutes",
			n.ID().Value(),
		)
		return rule.Findings
	}

	if n.TimeoutMinutes().Expression().Present() {
		return rule.Findings
	}
	if policy.Min != nil && n.TimeoutMinutes().Value() < *policy.Min {
		limit := *policy.Min
		rule.Errorf(
			reporting.ViewPosition(n.TimeoutMinutes().Pos()),
			"%q is %v in job %q. it must not be smaller than %v because \"min-minutes\" of \"job-timeout\" is set in the \"policy\" configuration",
			"timeout-minutes", n.TimeoutMinutes().Value(), n.ID().Value(), limit,
		)
	}
	if policy.Max != nil && n.TimeoutMinutes().Value() > *policy.Max {
		limit := *policy.Max
		rule.Errorf(
			reporting.ViewPosition(n.TimeoutMinutes().Pos()),
			"%q is %v in job %q. it must not be larger than %v because \"max-minutes\" of \"job-timeout\" is set in the \"policy\" configuration",
			"timeout-minutes",
			n.TimeoutMinutes().Value(),
			n.ID().Value(),
			limit,
		)
	}
	return rule.Findings
}
