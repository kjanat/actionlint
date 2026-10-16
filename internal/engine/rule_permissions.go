package engine

import (
	"actionlint.kjanat.dev/internal/checks/permission"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

var allPermissionScopes = permission.Scopes()

// RulePermissions is a rule checker to check permission configurations in a workflow.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#defining-access-for-the-github_token-scopes
type RulePermissions struct {
	RuleBase
}

// NewRulePermissions creates new RulePermissions instance.
func NewRulePermissions() *RulePermissions {
	return &RulePermissions{
		RuleBase: builtinRuleBase("permissions"),
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RulePermissions) VisitJobPre(n *workflowlang.Job) error {
	rule.checkPermissions(n.Permissions)
	return nil
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RulePermissions) VisitWorkflowPre(n *workflowlang.Workflow) error {
	rule.checkPermissions(n.Permissions)
	return nil
}

func (rule *RulePermissions) checkPermissions(p *workflowlang.Permissions) {
	if p == nil {
		return
	}
	input := permission.Declaration{}
	if p.All != nil {
		input.All = &permission.Value{Text: p.All.Value, Start: factPosition(p.All.Pos)}
	}
	for _, scope := range p.Scopes {
		input.Scopes = append(input.Scopes, permission.Scope{Name: permission.Value{Text: scope.Name.Value, Start: factPosition(scope.Name.Pos)}, Level: permission.Value{Text: scope.Value.Value, Start: factPosition(scope.Value.Pos)}})
	}
	for _, finding := range permission.Check(input) {
		rule.errs = append(rule.errs, legacyDiagnosticError(finding))
	}
}
