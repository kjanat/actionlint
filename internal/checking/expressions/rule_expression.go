package expressions

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/checking/binding"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/lookup"
	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	runner "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// Rule is the single definition of the expression rule.
var Rule = &define.Rule{Name: "expression", Description: "Validate expression values assigned to workflow fields", Category: "correctness", Subcategory: "expressions", Recommended: true, Help: "Use the expression value and interpolation form required by this workflow field. Syntax, references, context availability, calls, operand types, payload properties and script injection have separate rules.", SupportsActions: true,
	Prepare: define.PrepareWorkflowMetadata,
	Walker: func(c define.Inputs, base workflowvisit.Base) workflowvisit.Rule {
		return NewExpressionRule(c.Context, base, c.Actions.Prepared(), c.Workflows, c.Runners, c.RemoteActions)
	},
}

// ruleExpression connects workflow expression checking to host lookups and options.
type ruleExpression struct {
	*Checker
	reporting.Logger
	name, description string
	bindings          *binding.Scope
	host              repository.Snapshot
	frames            []expressionInvocation
	findings          workflowvisit.Findings
	ctx               context.Context
	remote            *actionremote.ResolvedActions
}

type expressionInvocation struct {
	scope  workflowvisit.Invocation
	parent *Checker
}

func (r *ruleExpression) EnterInvocation(scope workflowvisit.Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	r.frames = append(r.frames, expressionInvocation{scope: scope, parent: r.Checker})
	r.Checker = New(Dependencies{Action: lookup.New(r.ctx, scope.Actions, r.remote).Lookup}, workflowExpressionOptionsWithFacts(scope.Bindings, r.host))
	if body, available := scope.Source.ActionView(); available {
		r.BeginComposite(body)
	}
	return nil
}

func (r *ruleExpression) LeaveInvocation(context invocation.Context) error {
	if len(r.frames) == 0 || r.frames[len(r.frames)-1].scope.Context != context {
		return invocation.ErrForeignContext
	}
	frame := r.frames[len(r.frames)-1]
	if body, available := frame.scope.Source.ActionView(); available {
		mark := r.Checker.FindingCount()
		r.CheckCompositeOutputs(body)
		r.findings.Report(r.FindingsSince(mark)...)
	}
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

func (r *ruleExpression) VisitStep(step workflowdecl.Step) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitStep(step))
}

func (r *ruleExpression) VisitInvocationStep(step workflowdecl.Step) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.CheckCompositeStep(step))
}

func (r *ruleExpression) VisitExpandedAction(step workflowdecl.Step) error {
	return r.VisitInvocationStep(step)
}

func (r *ruleExpression) VisitOpaqueAction(step workflowdecl.Step) error {
	if len(r.frames) == 0 {
		// Workflow steps already receive the full expression pass before resolution.
		return nil
	}
	return r.VisitInvocationStep(step)
}

func (r *ruleExpression) VisitWorkflowPre(view workflowdecl.Workflow) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitWorkflowPre(view))
}

func (r *ruleExpression) VisitWorkflowPost(view workflowdecl.Workflow) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitWorkflowPost(view))
}

func (r *ruleExpression) VisitJobPre(view workflowdecl.Job) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitJobPre(view))
}

func (r *ruleExpression) VisitJobPost(view workflowdecl.Job) error {
	mark := r.Checker.FindingCount()
	return r.collect(mark, r.Checker.VisitJobPost(view))
}

// NewExpressionRule connects expression checking to the supplied dependency lookups and runner facts.
func NewExpressionRule(ctx context.Context, base workflowvisit.Base, actionsCache workspaceactions.MetadataLookup, workflowCache *workflowinputs.ResolvedWorkflows, runners *runner.Knowledge, remote *actionremote.ResolvedActions) *ruleExpression {
	r := &ruleExpression{Tag: base.Name(), name: base.Name(), description: base.Description(), ctx: ctx, remote: remote}
	r.Checker = New(Dependencies{
		Runners:  runners,
		Action:   lookup.New(ctx, actionsCache, remote).Lookup,
		Workflow: workflowCache.LookupForCall,
		Log:      r.Debug,
	}, workflowExpressionOptions(nil))
	return r
}

func (r *ruleExpression) Name() string                  { return r.name }
func (r *ruleExpression) Description() string           { return r.description }
func (r *ruleExpression) Errs() []*findingmodel.Finding { return r.findings.Errs() }

// FindingCount counts the findings collected by the rule. It shadows the
// embedded checker's count, which covers only the current invocation.
func (r *ruleExpression) FindingCount() int { return r.findings.FindingCount() }

func (r *ruleExpression) AnnotateSince(mark int, annotate func(*findingmodel.Finding)) {
	r.findings.AnnotateSince(mark, annotate)
}

func (r *ruleExpression) SetBindings(scope *binding.Scope) {
	r.bindings = scope
	r.Configure(workflowExpressionOptionsWithFacts(scope, r.host))
}

func (r *ruleExpression) SetRepository(facts repository.Snapshot) {
	r.host = facts
	r.Configure(workflowExpressionOptionsWithFacts(r.bindings, facts))
}

func workflowExpressionOptions(scope *binding.Scope) Options {
	return workflowExpressionOptionsWithFacts(scope, repository.Snapshot{})
}

func workflowExpressionOptionsWithFacts(scope *binding.Scope, host repository.Snapshot) Options {
	var config *configuration.Config
	if scope != nil {
		config = scope.Composite()
	}
	facts := configuration.RepositoryFacts(config, host)
	level := func(name string) configuration.Level {
		selected := config
		if scope != nil {
			selected = scope.Rule(name).Config()
		}
		return selected.RuleSet().DiagnosticLevel(selected, name)
	}
	return Options{
		Semantics:                 exprlang.Options{Variables: configuration.VariableNames(facts), Secrets: configuration.SecretNames(facts)},
		SuspiciousCoercion:        level("coercion") != "off",
		CaseInsensitiveConditions: level("case-insensitive-conditions") != "off",
		StringConditions:          level("string-conditions") != "off",
		JoinLiteral:               level("join-literal") != "off",
		CollectionComparison:      level("collection-comparison") != "off",
		MixedTypeComparisons:      level("mixed-type-comparisons") != "off",
		JobReferences:             func(job workflowdecl.Job) ReferenceNames { return jobReferenceNames(facts, job) },
		UnsoundTernary:            level("unsound-ternary") != "off",
		ScriptInjectionDisabled:   level("script-injection") == "off",
	}
}
