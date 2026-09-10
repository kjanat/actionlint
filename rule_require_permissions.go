package actionlint

// RuleRequirePermissions checks for explicit permissions at the configured scope.
type RuleRequirePermissions struct {
	RuleBase
	policy *PermissionsPolicy
}

// NewRuleRequirePermissions creates a rule for the given policy.
func NewRuleRequirePermissions(policy *PermissionsPolicy) *RuleRequirePermissions {
	return &RuleRequirePermissions{
		RuleBase: builtinRuleBase("require-permissions"),
		policy:   policy,
	}
}

// VisitWorkflowPre checks the workflow declaration when workflow scope is enabled.
func (rule *RuleRequirePermissions) VisitWorkflowPre(n *Workflow) error {
	if rule.policy.Scope() == "workflow" && n.Permissions == nil {
		rule.Errorf(&Pos{Line: 1, Col: 1}, "workflow-level \"permissions\" is required by the \"require-permissions\" policy. set \"permissions: {}\" and grant the scopes your jobs need")
	}
	return nil
}

// VisitJobPre checks each job declaration when job scope is enabled.
func (rule *RuleRequirePermissions) VisitJobPre(n *Job) error {
	if rule.policy.Scope() == "job" && n.Permissions == nil {
		rule.Errorf(n.Pos, "\"permissions\" is required in job %q by the \"require-permissions\" policy with \"scope: job\". set \"permissions: {}\" or grant the scopes this job needs", n.ID.Value)
	}
	return nil
}
