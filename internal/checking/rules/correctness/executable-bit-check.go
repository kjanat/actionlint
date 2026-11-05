package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	actionlocal "actionlint.kjanat.dev/v2/internal/model/actions/local"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ruleExecutableBit adapts workflow execution checks to traversal and composite scopes.
type ruleExecutableBit struct {
	*ExecutableChecker
	reporting.Logger
	frames   []executableInvocation
	findings workflowvisit.Findings
}

// NewRuleExecutableBit checks script executable bits using the supplied file and Git-index readers.
func NewRuleExecutableBit(input ExecutableContext) workflowvisit.Rule {
	descriptor := ExecutableBitDeclaration()
	r := &ruleExecutableBit{Tag: descriptor.Name}
	input.Log = r.Debug
	input.Report = func(finding *findingmodel.Finding) { r.findings.Report(finding) }
	r.ExecutableChecker = NewExecutableChecker(input)
	return r
}
func (rule *ruleExecutableBit) Name() string { return "executable-bit" }
func (rule *ruleExecutableBit) Description() string {
	return ExecutableBitDeclaration().Description
}

func (rule *ruleExecutableBit) Errs() []*findingmodel.Finding { return rule.findings.Errs() }
func (rule *ruleExecutableBit) FindingCount() int             { return rule.findings.FindingCount() }

func (rule *ruleExecutableBit) AnnotateSince(mark int, annotate func(*findingmodel.Finding)) {
	rule.findings.AnnotateSince(mark, annotate)
}
func (rule *ruleExecutableBit) VisitWorkflowPost(workflowdecl.WorkflowView) error { return nil }
func (rule *ruleExecutableBit) VisitJobPost(workflowdecl.JobView) error           { return nil }
func (rule *ruleExecutableBit) ApplyCheckout(actions *actionlocal.Resolver) {
	rule.SetPaths(actions.ApplyCheckout(rule.Paths()))
}

type executableInvocation struct {
	scope  checkcalls.Invocation
	parent *ExecutableChecker
}

func (rule *ruleExecutableBit) VisitStep(step workflowdecl.StepView) error {
	if _, action := step.Action(); action {
		return nil
	}
	return rule.VisitInvocationStep(step)
}

func (rule *ruleExecutableBit) VisitInvocationStep(step workflowdecl.StepView) error {
	return rule.ExecutableChecker.VisitStep(step)
}

func (rule *ruleExecutableBit) VisitOpaqueAction(step workflowdecl.StepView) error {
	return rule.VisitInvocationStep(step)
}

func (rule *ruleExecutableBit) EnterInvocation(scope checkcalls.Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	rule.frames = append(rule.frames, executableInvocation{scope: scope, parent: rule.ExecutableChecker})
	rule.ExecutableChecker = rule.ForkComposite(scope.Context, scope.ActionPath, rule.Paths())
	return nil
}

func (rule *ruleExecutableBit) LeaveInvocation(context invocation.Context) error {
	if len(rule.frames) == 0 || rule.frames[len(rule.frames)-1].scope.Context != context {
		return invocation.ErrForeignContext
	}
	frame := rule.frames[len(rule.frames)-1]
	frame.parent.JoinComposite(context, rule.ExecutableChecker)
	rule.ExecutableChecker = frame.parent
	rule.frames = rule.frames[:len(rule.frames)-1]
	return nil
}

var _ checkcalls.InvocationParticipant = (*ruleExecutableBit)(nil)
