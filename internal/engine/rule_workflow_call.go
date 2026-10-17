package engine

import (
	"strings"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/cachepolicy"
	"actionlint.kjanat.dev/internal/workflow/calls"
	"actionlint.kjanat.dev/internal/workflow/references"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// RuleWorkflowCall is a rule checker to check workflow call at jobs.<job_id>.
type RuleWorkflowCall struct {
	workflowvisit.Base
	ruleLogger
	ruleConfiguration
	workflowPath        string
	workflowPermissions *workflowlang.Permissions
	workflowCacheMode   *workflowlang.CacheMode
	cache               *calls.Cache
}

// NewRuleWorkflowCall creates a new RuleWorkflowCall instance. 'workflowPath' is a file path to
// the workflow which is relative to a project root directory or an absolute path.
func NewRuleWorkflowCall(workflowPath string, cache *calls.Cache) *RuleWorkflowCall {
	return &RuleWorkflowCall{
		Base: builtinRuleBase("workflow-call"), tag: "workflow-call",
		workflowPath: workflowPath,
		cache:        cache,
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleWorkflowCall) VisitWorkflowPre(n *workflowlang.Workflow) error {
	rule.workflowPermissions = n.Permissions
	rule.workflowCacheMode = n.CacheMode
	for _, e := range n.On {
		if e, ok := e.(*workflowlang.WorkflowCallEvent); ok {
			// Register this reusable workflow in cache so that it does not need to parse this workflow
			// file again when this workflow is called by other workflows.
			rule.cache.WriteWorkflowCallEventFromWorkflow(rule.workflowPath, e, n)
			break
		}
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleWorkflowCall) VisitJobPre(n *workflowlang.Job) error {
	if n.WorkflowCall == nil {
		return nil
	}

	u := n.WorkflowCall.Uses
	if u == nil || u.Value == "" || u.ContainsExpression() {
		return nil
	}

	if _, ok := references.LocalWorkflow(u.Value); ok {
		rule.checkWorkflowCallUsesLocal(n.WorkflowCall, n.Permissions, n.CacheMode)
		return nil
	}

	if _, ok := references.WorkflowRef(u.Value); ok {
		return nil
	}

	if strings.HasPrefix(u.Value, "./") || strings.HasPrefix(u.Value, "$/") {
		// When the specification is invalid and it is local reusable workflow call, remember it caused
		// an error by setting `nil` to cache. This can prevent redundant 'could not read workflow call'
		// error.
		rule.cache.IgnoreInvalid(u.Value)
	}

	rule.Errorf(
		u.Pos,
		"reusable workflow call %q at \"uses\" is not following the format \"owner/repo/path/to/workflow.yml@ref\", \"./path/to/workflow.yml\", nor \"$/path/to/workflow.yml\". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details",
		u.Value,
	)
	return nil
}

func (rule *RuleWorkflowCall) checkWorkflowCallUsesLocal(call *workflowlang.WorkflowCall, jobPerms *workflowlang.Permissions, jobCacheMode *workflowlang.CacheMode) {
	u := call.Uses
	m, err := rule.cache.FindMetadataForCall(u.Value)
	if err != nil {
		rule.Findings = append(rule.Findings, workflowLookupError(u, err))
		return
	}
	if m == nil {
		rule.Debug("Skip workflow call %q since no metadata was found", u.Value)
		return
	}

	for _, d := range calls.CheckArguments(call, m) {
		rule.Findings = append(rule.Findings, findingmodel.FromDiagnostic(d))
	}

	rule.checkWorkflowCallPermissions(call, jobPerms, m)
	for _, d := range cachepolicy.CheckCeiling(call, workflowlang.EffectiveCacheMode(rule.workflowCacheMode, jobCacheMode), m, rule.cache, rule.Debug) {
		rule.Findings = append(rule.Findings, findingmodel.FromDiagnostic(d))
	}

	rule.Debug("Validated reusable workflow %q", u.Value)
}

func (rule *RuleWorkflowCall) checkWorkflowCallPermissions(call *workflowlang.WorkflowCall, jobPerms *workflowlang.Permissions, m *calls.Metadata) {
	var granted calls.ResolvedPermissions
	switch {
	case jobPerms != nil:
		granted = calls.ResolvePermissionsAST(jobPerms)
	case rule.workflowPermissions != nil:
		granted = calls.ResolvePermissionsAST(rule.workflowPermissions)
	default:
		a := DefaultPermissionsAssumptionUnset
		if cfg := rule.Config(); cfg != nil {
			a = cfg.AssumeDefaultPermissions
		}
		granted = calls.ResolvedPermissions{Kind: calls.PermissionsDeclared, Levels: calls.DefaultPermissionLevels(a == DefaultPermissionsAssumptionPermissive)}
	}

	for _, d := range calls.CheckPermissions(call, granted, m) {
		rule.Findings = append(rule.Findings, findingmodel.FromDiagnostic(d))
	}
}

// workflowLookupError assigns dependency failures to their owning rule, even
// when expression inference requested the same metadata. Selection and
// suppression consequently do not depend on which consumer ran first.
func workflowLookupError(uses *workflowlang.String, err error) *findingmodel.Finding {
	return findingmodel.At(uses.Pos, "workflow-call", err.Error())
}
