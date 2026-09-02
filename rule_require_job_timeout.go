package actionlint

// RuleRequireJobTimeout is a rule to check that every job sets "timeout-minutes:". The
// "require-job-timeout" policy in the configuration file enables it, and its "max-minutes" key also
// caps the value.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idtimeout-minutes
type RuleRequireJobTimeout struct {
	RuleBase
	policy *JobTimeoutPolicy
}

// NewRuleRequireJobTimeout creates a new RuleRequireJobTimeout instance with the given policy.
func NewRuleRequireJobTimeout(policy *JobTimeoutPolicy) *RuleRequireJobTimeout {
	return &RuleRequireJobTimeout{
		name:   "require-job-timeout",
		desc:   "Checks that every job sets \"timeout-minutes:\" within the configured maximum",
		policy: policy,
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleRequireJobTimeout) VisitJobPre(n *Job) error {
	if !rule.policy.Enabled() {
		return nil
	}

	// A job which calls a reusable workflow cannot set "timeout-minutes:".
	// https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations#supported-keywords-for-jobs-that-call-a-reusable-workflow
	if n.WorkflowCall != nil || n.Steps == nil {
		return nil
	}

	if n.TimeoutMinutes == nil {
		rule.Errorf(
			n.Pos,
			"%q is not set in job %q. it is required because \"require-job-timeout\" is enabled in the \"policy\" configuration. a job which does not set it is cancelled after the default of 360 minutes",
			"timeout-minutes",
			n.ID.Value,
		)
		return nil
	}

	limit, ok := rule.policy.MaxMinutes()
	if ok && n.TimeoutMinutes.Expression == nil && n.TimeoutMinutes.Value > limit {
		rule.Errorf(
			n.TimeoutMinutes.Pos,
			"%q is %v in job %q. it must not be larger than %v because \"max-minutes\" of \"require-job-timeout\" is set in the \"policy\" configuration",
			"timeout-minutes",
			n.TimeoutMinutes.Value,
			n.ID.Value,
			limit,
		)
	}
	return nil
}
