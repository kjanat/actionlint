package builtin

import (
	"errors"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/actioncatalog"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowexpr "actionlint.kjanat.dev/v2/internal/workflow/expressions"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// RuleExpression connects workflow expression checking to host lookups and options.
type RuleExpression struct {
	*workflowexpr.Checker
	ruleLogger
	Configuration
	frames   []expressionInvocation
	findings workflowvisit.Findings
}

type expressionInvocation struct {
	scope  Invocation
	parent *workflowexpr.Checker
}

func (r *RuleExpression) EnterInvocation(scope Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	r.frames = append(r.frames, expressionInvocation{scope: scope, parent: r.Checker})
	r.Checker = workflowexpr.New(workflowexpr.Dependencies{}, workflowExpressionOptions(scope.Config))
	return nil
}
func (r *RuleExpression) LeaveInvocation(context invocation.Context) error {
	if len(r.frames) == 0 || r.frames[len(r.frames)-1].scope.Context != context {
		return invocation.ErrForeignContext
	}
	frame := r.frames[len(r.frames)-1]
	r.Checker = frame.parent
	r.frames = r.frames[:len(r.frames)-1]
	return nil
}
func (r *RuleExpression) collect(visit func() error) error {
	before := r.Checker.FindingCount()
	err := visit()
	r.findings.Report(r.FindingsSince(before)...)
	return err
}
func (r *RuleExpression) VisitStep(step workflowdecl.StepView) error {
	return r.collect(func() error { return r.Checker.VisitStep(step) })
}
func (r *RuleExpression) VisitInvocationStep(step workflowdecl.StepView) error {
	return r.collect(func() error { r.CheckCompositeScript(step); return nil })
}
func (r *RuleExpression) VisitOpaqueAction(step workflowdecl.StepView) error {
	if len(r.frames) == 0 {
		// Workflow steps already receive the full expression pass before resolution.
		return nil
	}
	return r.VisitInvocationStep(step)
}
func (r *RuleExpression) VisitWorkflowPre(view workflowdecl.WorkflowView) error {
	return r.collect(func() error { return r.Checker.VisitWorkflowPre(view) })
}
func (r *RuleExpression) VisitWorkflowPost(view workflowdecl.WorkflowView) error {
	return r.collect(func() error { return r.Checker.VisitWorkflowPost(view) })
}
func (r *RuleExpression) VisitJobPre(view workflowdecl.JobView) error {
	return r.collect(func() error { return r.Checker.VisitJobPre(view) })
}
func (r *RuleExpression) VisitJobPost(view workflowdecl.JobView) error {
	return r.collect(func() error { return r.Checker.VisitJobPost(view) })
}

func NewRuleExpression(actionsCache actionlocal.MetadataLookup, workflowCache *calls.Cache) *RuleExpression {
	return newRuleExpression(actionsCache, workflowCache, nil)
}

func newRuleExpression(actionsCache actionlocal.MetadataLookup, workflowCache *calls.Cache, runners *runner.Knowledge) *RuleExpression {
	r := &RuleExpression{tag: "expression"}
	r.Checker = workflowexpr.New(workflowexpr.Dependencies{
		Runners: runners,
		LocalAction: func(spec string) (*actionmeta.Metadata, error) {
			if actionsCache == nil {
				return nil, nil
			}
			meta, _, err := actionsCache.FindMetadata(spec)
			// Action validation owns unresolved references and explicit mapping failures.
			if _, unresolved := errors.AsType[actionlocal.UnresolvedError](err); unresolved {
				return nil, nil
			}
			if _, mapping := errors.AsType[actionlocal.MappingError](err); mapping {
				return nil, nil
			}
			return meta, err
		},
		PopularAction: func(spec string) *actionmeta.Metadata { meta, _ := actioncatalog.Lookup(spec); return meta },
		Workflow:      workflowCache.FindMetadataForCall,
		Log:           r.Debug,
	}, workflowExpressionOptions(nil))
	return r
}

func (r *RuleExpression) Name() string                  { return "expression" }
func (r *RuleExpression) Description() string           { return builtinRuleDescription("expression") }
func (r *RuleExpression) Errs() []*findingmodel.Finding { return r.findings.Errs() }

// FindingCount counts the findings collected by the rule. It shadows the
// embedded checker's count, which covers only the current invocation.
func (r *RuleExpression) FindingCount() int { return r.findings.FindingCount() }
func (r *RuleExpression) AnnotateSince(mark int, annotate func(*findingmodel.Finding)) {
	r.findings.AnnotateSince(mark, annotate)
}
func (r *RuleExpression) SetConfig(config *repoconfig.Config) {
	r.Configuration.SetConfig(config)
	r.Configure(workflowExpressionOptions(config))
}

func workflowExpressionOptions(config *repoconfig.Config) workflowexpr.Options {
	return workflowexpr.Options{
		Semantics:               *expressionOptionsFromConfig(config),
		UnsoundTernary:          repoconfig.DiagnosticLevel(config, "unsound-ternary") != "off",
		ScriptInjectionDisabled: repoconfig.DiagnosticLevel(config, "script-injection") == "off",
	}
}

func expressionOptionsFromConfig(config *repoconfig.Config) *exprlang.Options {
	options := &exprlang.Options{Checks: exprlang.Checks{
		StringConditions:          repoconfig.DiagnosticLevel(config, "string-conditions") != "off",
		MixedTypeComparisons:      repoconfig.DiagnosticLevel(config, "mixed-type-comparisons") != "off",
		CaseInsensitiveConditions: repoconfig.DiagnosticLevel(config, "case-insensitive-conditions") != "off",
	}}
	if config != nil {
		options.Variables, options.Secrets = config.ConfigVariables, config.ConfigSecrets
	}
	return options
}
