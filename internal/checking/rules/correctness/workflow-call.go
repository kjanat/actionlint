package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/cachepolicy"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/workflowcall"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references/reusableworkflows"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/cachegrant"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// WorkflowCallRule is the single definition of the workflow-call rule.
var WorkflowCallRule = &define.Rule{
	SupportsActions: true,
	Name:            "workflow-call",
	Description:     "Validate reusable workflow references and their declared inputs and outputs",
	Category:        "correctness",
	Subcategory:     "workflows",
	MovedOptions:    []rule.MovedOption{{Option: "default-permissions", Repository: "default-workflow-permissions"}},
	Recommended:     true,
	Explanation:     "Check reusable workflow references and report missing or undeclared inputs and secrets using available local or remote metadata. Compare the caller's known token permissions with the called workflow's declared job permissions. Cache limits are checked for local calls. The expression-reference rule checks references to workflow outputs.",
	Help:            "Correct the reference and supply required inputs and secrets. For permission or cache conflicts, adjust the caller's grant or reduce the called workflow's requirements.",
	Prepare:         define.PrepareWorkflowMetadata,
	Checks: func(c define.RuleInputs) define.Checks {
		workflow, _ := c.SourceSnapshot.WorkflowView()
		logger := reporting.Logger{Tag: "workflow-call", Writer: c.Debug}
		return WorkflowCallChecks(workflow, c.Workflows, configuration.RepositoryFacts(c.Config, c.Repository), logger.Debug)
	},
}

type workflowCallCheck struct {
	reporting.Collector
	defaultPermissions  calls.ResolvedPermissions
	workflowPermissions *definition.Permissions
	workflowCacheMode   *definition.CacheMode
	cache               *reusableworkflows.ResolvedWorkflows
	log                 func(string, ...any)
}

// WorkflowCallChecks validates each reusable call against supplied workflow
// defaults, repository facts and loaded dependency metadata. Source preparation
// seeds the metadata cache before callbacks run. A nil log disables debug output.
func WorkflowCallChecks(workflow workflows.BoundWorkflow, resolvedWorkflows *reusableworkflows.ResolvedWorkflows, facts repository.Settings, log func(string, ...any)) define.Checks {
	var defaults calls.ResolvedPermissions
	if permissions, known := facts.DefaultWorkflowPermissions.Value(); known {
		defaults = calls.ResolvedPermissions{Kind: calls.PermissionsDeclared, Levels: calls.DefaultPermissionLevels(permissions.String() == "write")}
	}
	return define.Checks{
		Job: func(job workflows.BoundJob) []reporting.StepFinding {
			check := workflowCallCheck{
				workflowPermissions: workflow.Permissions, workflowCacheMode: workflow.CacheMode,
				cache: resolvedWorkflows, defaultPermissions: defaults, log: log,
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

func (rule *workflowCallCheck) checkJob(n workflows.BoundJob) {
	if n.WorkflowCall == nil {
		return
	}

	u := n.WorkflowCall.Uses
	if u == nil || u.Value == "" || u.ContainsExpression() {
		return
	}
	if _, hosted := callreferences.ParseRepositoryURL(u.Value); hosted {
		return // vendor-lock-in reports URL-form uses declarations.
	}

	if _, ok := callreferences.LocalWorkflow(u.Value); ok {
		rule.checkWorkflowCall(n.WorkflowCall, n.Permissions, n.CacheMode)
		return
	}

	if _, ok := callreferences.WorkflowRef(u.Value); ok {
		rule.checkWorkflowCall(n.WorkflowCall, n.Permissions, n.CacheMode)
		return
	}

	rule.Errorf(
		reporting.SourcePosition(u),
		"reusable workflow call %q at \"uses\" is not following the format \"owner/repo/path/to/workflow.yml@ref\", \"./path/to/workflow.yml\", nor \"$/path/to/workflow.yml\". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details",
		u.Value,
	)
}

func (rule *workflowCallCheck) checkWorkflowCall(call *definition.WorkflowCall, jobPerms *definition.Permissions, jobCacheMode *definition.CacheMode) {
	u := call.Uses
	m, err := rule.cache.LookupForCall(u.Value)
	if err != nil {
		// Lookup failures belong to this rule even if inference loaded them first.
		rule.Error(reporting.SourcePosition(u), err.Error())
		return
	}
	if m == nil {
		rule.debug("Skip workflow call %q since no metadata was found", u.Value)
		return
	}

	rule.Findings = append(rule.Findings, reporting.FromDiagnostics(workflowcall.CheckArguments(call, m))...)

	rule.checkWorkflowCallPermissions(call, jobPerms, m)
	rule.Findings = append(rule.Findings, reporting.FromDiagnostics(cachepolicy.CheckCeiling(call, cachegrant.EffectiveMode(rule.workflowCacheMode, jobCacheMode), m, rule.cache, rule.debug))...)

	rule.debug("Validated reusable workflow %q", u.Value)
}

func (rule *workflowCallCheck) checkWorkflowCallPermissions(call *definition.WorkflowCall, jobPerms *definition.Permissions, m *calls.Metadata) {
	granted := calls.EffectivePermissions(rule.workflowPermissions, jobPerms, rule.defaultPermissions)

	rule.Findings = append(rule.Findings, reporting.FromDiagnostics(workflowcall.CheckPermissions(call, granted, m))...)
}
