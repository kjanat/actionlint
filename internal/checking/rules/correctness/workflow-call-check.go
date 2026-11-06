package correctness

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	cachemodel "actionlint.kjanat.dev/v2/internal/model/workflows/cache"

	workflowcall "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/workflow-call"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"

	cachepolicy "actionlint.kjanat.dev/v2/internal/checking/cache"

	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

type workflowCallCheck struct {
	reporting.Collector
	defaultPermissions  calls.ResolvedPermissions
	workflowPermissions workflowdecl.PermissionsView
	workflowCacheMode   workflowdecl.CacheModeView
	cache               *workflowinputs.ResolvedWorkflows
	log                 func(string, ...any)
}

// WorkflowCallCheck validates each reusable call against supplied workflow
// defaults, repository facts and loaded dependency metadata. Source preparation
// seeds the metadata cache before callbacks run. A nil log disables debug output.
func WorkflowCallCheck(workflow workflowdecl.WorkflowView, cache *workflowinputs.ResolvedWorkflows, facts repository.Snapshot, log func(string, ...any)) checkinputs.DeclarationCheck {
	descriptor := WorkflowCallDeclaration()
	var defaults calls.ResolvedPermissions
	if permissions, known := facts.DefaultWorkflowPermissions.Value(); known {
		defaults = calls.ResolvedPermissions{Kind: calls.PermissionsDeclared, Levels: calls.DefaultPermissionLevels(permissions.String() == "write")}
	}
	return checkinputs.DeclarationCheck{
		Declaration: descriptor,
		Job: func(job workflowdecl.JobView) []reporting.Finding {
			check := workflowCallCheck{
				Rule:                descriptor.Name,
				workflowPermissions: workflow.Permissions(), workflowCacheMode: workflow.CacheMode(),
				cache: cache, defaultPermissions: defaults, log: log,
			}
			check.checkJob(job)
			return check.Findings
		},
	}
}

func (rule *workflowCallCheck) debug(format string, args ...any) {
	if rule.log != nil {
		rule.log(format, args...)
	}
}

func (rule *workflowCallCheck) checkJob(n workflowdecl.JobView) {
	if !n.WorkflowCall().Present() {
		return
	}

	u := n.WorkflowCall().Uses()
	if !u.Present() || u.Value() == "" || u.ContainsExpression() {
		return
	}
	if _, hosted := references.ParseRepositoryURL(u.Value()); hosted {
		return // vendor-lock-in reports URL-form uses declarations.
	}

	if _, ok := references.LocalWorkflow(u.Value()); ok {
		rule.checkWorkflowCallUsesLocal(n.WorkflowCall(), n.Permissions(), n.CacheMode())
		return
	}

	if _, ok := references.WorkflowRef(u.Value()); ok {
		return
	}

	rule.Errorf(
		reporting.SourcePosition(u),
		"reusable workflow call %q at \"uses\" is not following the format \"owner/repo/path/to/workflow.yml@ref\", \"./path/to/workflow.yml\", nor \"$/path/to/workflow.yml\". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details",
		u.Value(),
	)
}

func (rule *workflowCallCheck) checkWorkflowCallUsesLocal(call workflowdecl.WorkflowCallView, jobPerms workflowdecl.PermissionsView, jobCacheMode workflowdecl.CacheModeView) {
	u := call.Uses()
	m, err := rule.cache.LookupForCall(u.Value())
	if err != nil {
		// Lookup failures belong to this rule even if inference loaded them first.
		rule.Error(reporting.SourcePosition(u), err.Error())
		return
	}
	if m == nil {
		rule.debug("Skip workflow call %q since no metadata was found", u.Value())
		return
	}

	rule.Findings = append(rule.Findings, reporting.FromDiagnostics(workflowcall.CheckArguments(call, m))...)

	rule.checkWorkflowCallPermissions(call, jobPerms, m)
	rule.Findings = append(rule.Findings, reporting.FromDiagnostics(cachepolicy.CheckCeiling(call, cachemodel.EffectiveMode(rule.workflowCacheMode, jobCacheMode), m, rule.cache, rule.debug))...)

	rule.debug("Validated reusable workflow %q", u.Value())
}

func (rule *workflowCallCheck) checkWorkflowCallPermissions(call workflowdecl.WorkflowCallView, jobPerms workflowdecl.PermissionsView, m *calls.Metadata) {
	granted := calls.EffectivePermissions(rule.workflowPermissions, jobPerms, rule.defaultPermissions)

	rule.Findings = append(rule.Findings, reporting.FromDiagnostics(workflowcall.CheckPermissions(call, granted, m))...)
}
