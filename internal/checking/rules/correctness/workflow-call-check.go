package correctness

import (
	"strings"

	cachemodel "actionlint.kjanat.dev/v2/internal/model/workflows/cache"

	workflowcall "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/workflow-call"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"

	cachepolicy "actionlint.kjanat.dev/v2/internal/checking/cache"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ruleWorkflowCall is a rule checker to check workflow call at jobs.<job_id>.
type ruleWorkflowCall struct {
	workflowvisit.Base
	reporting.Logger
	facts               repository.Snapshot
	workflowPermissions workflowdecl.PermissionsView
	workflowCacheMode   workflowdecl.CacheModeView
	cache               *calls.Cache
	source              parsing.SourceSnapshot
}

// NewRuleWorkflowCall checks calls using captured reusable-workflow metadata.
func NewRuleWorkflowCall(source parsing.SourceSnapshot, cache *calls.Cache, facts repository.Snapshot) workflowvisit.Rule {
	descriptor := WorkflowCallDeclaration()
	return &ruleWorkflowCall{
		Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description), Tag: descriptor.Name, facts: facts,
		cache:  cache,
		source: source,
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *ruleWorkflowCall) VisitWorkflowPre(n workflowdecl.WorkflowView) error {
	rule.workflowPermissions = n.Permissions()
	rule.workflowCacheMode = n.CacheMode()
	for event := range n.On() {
		if _, reusable := event.WorkflowCallEvent(); reusable {
			// Seed rejects an absent snapshot itself.
			if rule.source.Present() {
				if err := rule.source.ValidateWorkflow(n); err != nil {
					return err
				}
			}
			return rule.cache.Seed(rule.source)
		}
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *ruleWorkflowCall) VisitJobPre(n workflowdecl.JobView) error {
	if !n.WorkflowCall().Present() {
		return nil
	}

	u := n.WorkflowCall().Uses()
	if !u.Present() || u.Value() == "" || u.ContainsExpression() {
		return nil
	}
	if _, hosted := references.ParseRepositoryURL(u.Value()); hosted {
		return nil // vendor-lock-in reports URL-form uses declarations.
	}

	if _, ok := references.LocalWorkflow(u.Value()); ok {
		rule.checkWorkflowCallUsesLocal(n.WorkflowCall(), n.Permissions(), n.CacheMode())
		return nil
	}

	if _, ok := references.WorkflowRef(u.Value()); ok {
		return nil
	}

	if strings.HasPrefix(u.Value(), "./") || strings.HasPrefix(u.Value(), "$/") {
		// When the specification is invalid and it is local reusable workflow call, remember it caused
		// an error by setting `nil` to cache. This can prevent redundant 'could not read workflow call'
		// error.
		rule.cache.IgnoreInvalid(u.Value())
	}

	rule.Errorf(
		reporting.SourcePosition(u),
		"reusable workflow call %q at \"uses\" is not following the format \"owner/repo/path/to/workflow.yml@ref\", \"./path/to/workflow.yml\", nor \"$/path/to/workflow.yml\". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details",
		u.Value(),
	)
	return nil
}

func (rule *ruleWorkflowCall) checkWorkflowCallUsesLocal(call workflowdecl.WorkflowCallView, jobPerms workflowdecl.PermissionsView, jobCacheMode workflowdecl.CacheModeView) {
	u := call.Uses()
	m, err := rule.cache.FindMetadataForCall(u.Value())
	if err != nil {
		rule.Report(workflowLookupError(u, err))
		return
	}
	if m == nil {
		rule.Debug("Skip workflow call %q since no metadata was found", u.Value())
		return
	}

	for _, d := range workflowcall.CheckArguments(call, m) {
		rule.Report(findingmodel.FromDiagnostic(d))
	}

	rule.checkWorkflowCallPermissions(call, jobPerms, m)
	for _, d := range cachepolicy.CheckCeiling(call, cachemodel.EffectiveMode(rule.workflowCacheMode, jobCacheMode), m, rule.cache, rule.Debug) {
		rule.Report(findingmodel.FromDiagnostic(d))
	}

	rule.Debug("Validated reusable workflow %q", u.Value())
}

func (rule *ruleWorkflowCall) checkWorkflowCallPermissions(call workflowdecl.WorkflowCallView, jobPerms workflowdecl.PermissionsView, m *calls.Metadata) {
	var granted calls.ResolvedPermissions
	switch {
	case jobPerms.Present():
		granted = calls.ResolvePermissions(jobPerms)
	case rule.workflowPermissions.Present():
		granted = calls.ResolvePermissions(rule.workflowPermissions)
	default:
		if permissions, known := rule.facts.DefaultWorkflowPermissions.Value(); known {
			granted = calls.ResolvedPermissions{Kind: calls.PermissionsDeclared, Levels: calls.DefaultPermissionLevels(permissions.String() == "write")}
		}
	}

	for _, d := range workflowcall.CheckPermissions(call, granted, m) {
		rule.Report(findingmodel.FromDiagnostic(d))
	}
}

// workflowLookupError assigns dependency failures to their owning rule, even
// when expression inference requested the same metadata. Selection and
// suppression consequently do not depend on which consumer ran first.
func workflowLookupError(uses workflowdecl.StringView, err error) *findingmodel.Finding {
	return findingmodel.At(reporting.SourcePosition(uses), "workflow-call", err.Error())
}
