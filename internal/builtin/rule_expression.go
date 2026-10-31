package builtin

import (
	"errors"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	actionremote "actionlint.kjanat.dev/v2/internal/action/remote"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowexpr "actionlint.kjanat.dev/v2/internal/workflow/expressions"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/repository"
	"context"
)

// ruleExpression connects workflow expression checking to host lookups and options.
type ruleExpression struct {
	*workflowexpr.Checker
	ruleLogger
	configuration
	frames       []expressionInvocation
	findings     workflowvisit.Findings
	remoteLookup func(string) (actionmeta.Definition, error)
}

type expressionInvocation struct {
	scope  Invocation
	parent *workflowexpr.Checker
}

func (r *ruleExpression) EnterInvocation(scope Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	r.frames = append(r.frames, expressionInvocation{scope: scope, parent: r.Checker})
	r.Checker = workflowexpr.New(workflowexpr.Dependencies{RemoteAction: r.remoteLookup}, workflowExpressionOptionsWithFacts(scope.Config, r.host))
	return nil
}
func (r *ruleExpression) LeaveInvocation(context invocation.Context) error {
	if len(r.frames) == 0 || r.frames[len(r.frames)-1].scope.Context != context {
		return invocation.ErrForeignContext
	}
	frame := r.frames[len(r.frames)-1]
	r.Checker = frame.parent
	r.frames = r.frames[:len(r.frames)-1]
	return nil
}

// collect moves the findings the current checker reported after mark, a prior
// r.Checker.FindingCount, into the rule, and passes err through.
func (r *ruleExpression) collect(mark int, err error) error {
	r.findings.Report(r.FindingsSince(mark)...)
	return err
}
func (r *ruleExpression) VisitStep(step workflowdecl.StepView) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitStep(step))
}
func (r *ruleExpression) VisitInvocationStep(step workflowdecl.StepView) error {
	mark := r.Checker.FindingCount()
	r.CheckCompositeScript(step)
	return r.collect(mark, nil)
}
func (r *ruleExpression) VisitOpaqueAction(step workflowdecl.StepView) error {
	if len(r.frames) == 0 {
		// Workflow steps already receive the full expression pass before resolution.
		return nil
	}
	return r.VisitInvocationStep(step)
}
func (r *ruleExpression) VisitWorkflowPre(view workflowdecl.WorkflowView) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitWorkflowPre(view))
}
func (r *ruleExpression) VisitWorkflowPost(view workflowdecl.WorkflowView) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitWorkflowPost(view))
}
func (r *ruleExpression) VisitJobPre(view workflowdecl.JobView) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitJobPre(view))
}
func (r *ruleExpression) VisitJobPost(view workflowdecl.JobView) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitJobPost(view))
}

// NewRuleExpression checks expressions, looking up local action metadata in
// actionsCache and reusable-workflow metadata in workflowCache. It has no
// runner knowledge, so it does not learn the shell of a job from runs-on.
func NewRuleExpression(actionsCache actionlocal.MetadataLookup, workflowCache *calls.Cache) InvocationParticipant {
	return newRuleExpression(actionsCache, workflowCache, nil)
}

func newRuleExpression(actionsCache actionlocal.MetadataLookup, workflowCache *calls.Cache, runners *runner.Knowledge) *ruleExpression {
	return newRuleExpressionWithRemote(actionsCache, workflowCache, runners, nil, context.Background())
}

func newRuleExpressionWithRemote(actionsCache actionlocal.MetadataLookup, workflowCache *calls.Cache, runners *runner.Knowledge, remote *actionremote.Service, ctx context.Context) *ruleExpression {
	r := &ruleExpression{tag: "expression"}
	if remote != nil {
		r.remoteLookup = func(spec string) (actionmeta.Definition, error) { return remote.Lookup(ctx, spec) }
	}
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
		RemoteAction: r.remoteLookup,
		Workflow:     workflowCache.FindMetadataForCall,
		Log:          r.Debug,
	}, workflowExpressionOptions(nil))
	return r
}

func (r *ruleExpression) Name() string                  { return "expression" }
func (r *ruleExpression) Description() string           { return builtinRuleDescription("expression") }
func (r *ruleExpression) Errs() []*findingmodel.Finding { return r.findings.Errs() }

// FindingCount counts the findings collected by the rule. It shadows the
// embedded checker's count, which covers only the current invocation.
func (r *ruleExpression) FindingCount() int { return r.findings.FindingCount() }
func (r *ruleExpression) AnnotateSince(mark int, annotate func(*findingmodel.Finding)) {
	r.findings.AnnotateSince(mark, annotate)
}
func (r *ruleExpression) SetConfig(config *repoconfig.Config) {
	r.configuration.SetConfig(config)
	r.Configure(workflowExpressionOptionsWithFacts(config, r.host))
}

func (r *ruleExpression) SetRepository(facts repository.Snapshot) {
	r.configuration.SetRepository(facts)
	r.Configure(workflowExpressionOptionsWithFacts(r.config, facts))
}

func workflowExpressionOptions(config *repoconfig.Config) workflowexpr.Options {
	return workflowExpressionOptionsWithFacts(config, repository.Snapshot{})
}

func workflowExpressionOptionsWithFacts(config *repoconfig.Config, host repository.Snapshot) workflowexpr.Options {
	semantics := expressionOptionsFromConfig(config)
	facts := repoconfig.RepositoryFacts(config, host)
	semantics.Variables, semantics.Secrets = repoconfig.VariableNames(facts), repoconfig.SecretNames(facts)
	return workflowexpr.Options{
		Semantics:               *semantics,
		JobReferences:           func(job workflowdecl.JobView) workflowexpr.ReferenceNames { return jobReferenceNames(facts, job) },
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
	facts := repoconfig.RepositoryFacts(config, repository.Snapshot{})
	options.Variables, options.Secrets = repoconfig.VariableNames(facts), repoconfig.SecretNames(facts)
	return options
}
