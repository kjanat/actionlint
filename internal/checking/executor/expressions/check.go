package expressions

import (
	"context"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"
	workflowexpr "actionlint.kjanat.dev/v2/internal/checking/expressions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/lookup"
	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	runner "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ruleExpression connects workflow expression checking to host lookups and options.
type ruleExpression struct {
	*workflowexpr.Checker
	reporting.Logger
	config            *repoconfig.Config
	host              repository.Snapshot
	frames            []expressionInvocation
	findings          workflowvisit.Findings
	invocationActions func(string) (actionmeta.Definition, error)
}

type expressionInvocation struct {
	scope  checkcalls.Invocation
	parent *workflowexpr.Checker
}

func (r *ruleExpression) EnterInvocation(scope checkcalls.Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	r.frames = append(r.frames, expressionInvocation{scope: scope, parent: r.Checker})
	r.Checker = workflowexpr.New(workflowexpr.Dependencies{Action: r.invocationActions}, workflowExpressionOptionsWithFacts(scope.Config, r.host))
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

// New connects expression checking to the supplied dependency lookups and runner facts.
func New(ctx context.Context, actionsCache workspaceactions.MetadataLookup, workflowCache *workflowinputs.Cache, runners *runner.Knowledge, remote *actionremote.ResolvedActions) *ruleExpression {
	r := &ruleExpression{Tag: "expression"}
	r.invocationActions = lookup.New(ctx, nil, remote).Lookup
	r.Checker = workflowexpr.New(workflowexpr.Dependencies{
		Runners:  runners,
		Action:   lookup.New(ctx, actionsCache, remote).Lookup,
		Workflow: workflowCache.FindMetadataForCall,
		Log:      r.Debug,
	}, workflowExpressionOptions(nil))
	return r
}

func (r *ruleExpression) Name() string                  { return "expression" }
func (r *ruleExpression) Description() string           { return correctness.ExpressionDeclaration().Description }
func (r *ruleExpression) Errs() []*findingmodel.Finding { return r.findings.Errs() }

// FindingCount counts the findings collected by the rule. It shadows the
// embedded checker's count, which covers only the current invocation.
func (r *ruleExpression) FindingCount() int { return r.findings.FindingCount() }

func (r *ruleExpression) AnnotateSince(mark int, annotate func(*findingmodel.Finding)) {
	r.findings.AnnotateSince(mark, annotate)
}

func (r *ruleExpression) SetConfig(config *repoconfig.Config) {
	r.config = config
	r.Configure(workflowExpressionOptionsWithFacts(config, r.host))
}

func (r *ruleExpression) SetRepository(facts repository.Snapshot) {
	r.host = facts
	r.Configure(workflowExpressionOptionsWithFacts(r.config, facts))
}

func workflowExpressionOptions(config *repoconfig.Config) workflowexpr.Options {
	return workflowExpressionOptionsWithFacts(config, repository.Snapshot{})
}

func workflowExpressionOptionsWithFacts(config *repoconfig.Config, host repository.Snapshot) workflowexpr.Options {
	facts := repoconfig.RepositoryFacts(config, host)
	return workflowexpr.Options{
		Semantics:                 exprlang.Options{Variables: repoconfig.VariableNames(facts), Secrets: repoconfig.SecretNames(facts)},
		SuspiciousCoercion:        config.RuleSet().DiagnosticLevel(config, "coercion") != "off",
		CaseInsensitiveConditions: config.RuleSet().DiagnosticLevel(config, "case-insensitive-conditions") != "off",
		StringConditions:          config.RuleSet().DiagnosticLevel(config, "string-conditions") != "off",
		MixedTypeComparisons:      config.RuleSet().DiagnosticLevel(config, "mixed-type-comparisons") != "off",
		JobReferences:             func(job workflowdecl.JobView) workflowexpr.ReferenceNames { return jobReferenceNames(facts, job) },
		UnsoundTernary:            config.RuleSet().DiagnosticLevel(config, "unsound-ternary") != "off",
		ScriptInjectionDisabled:   config.RuleSet().DiagnosticLevel(config, "script-injection") == "off",
	}
}
