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

// JobTimeoutCheck copies the configured bounds and checks each job against them.
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
			"%q is not set in job %q. Set an explicit timeout for this job",
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
			"%q is %v in job %q, below the configured min-minutes of %v",
			"timeout-minutes", n.TimeoutMinutes().Value(), n.ID().Value(), limit,
		)
	}
	if policy.Max != nil && n.TimeoutMinutes().Value() > *policy.Max {
		limit := *policy.Max
		rule.Errorf(
			reporting.ViewPosition(n.TimeoutMinutes().Pos()),
			"%q is %v in job %q, above the configured max-minutes of %v",
			"timeout-minutes",
			n.TimeoutMinutes().Value(),
			n.ID().Value(),
			limit,
		)
	}
	return rule.Findings
}
