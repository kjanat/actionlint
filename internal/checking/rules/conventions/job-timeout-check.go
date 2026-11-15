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
	return checkinputs.DeclarationCheck{Declaration: JobTimeoutDeclaration(), Job: func(job declaration.Job) []reporting.StepFinding {
		return JobTimeout(job, policy)
	}}
}

func JobTimeout(n declaration.Job, policy TimeoutPolicy) []reporting.StepFinding {
	rule := reporting.Collector{Rule: "job-timeout"}

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
