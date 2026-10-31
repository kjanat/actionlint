package builtin

import (
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// ruleExecutableBit adapts workflow execution checks to traversal and composite scopes.
type ruleExecutableBit struct {
	*execution.ExecutableChecker
	ruleLogger
	frames   []executableInvocation
	findings workflowvisit.Findings
}

// NewRuleExecutableBit checks script executable bits under c.ProjectRoot,
// reading files through c.Reader and the git index through c.Index.
func NewRuleExecutableBit(c Context) *ruleExecutableBit {
	r := &ruleExecutableBit{tag: "executable-bit"}
	r.ExecutableChecker = execution.NewExecutableChecker(execution.ExecutableContext{
		Context: c.Context, ProjectRoot: c.ProjectRoot, WorkingDir: c.WorkingDir,
		Reader: c.Reader, Index: c.Index, Log: r.Debug, Runners: c.Runners,
		Report: func(finding *findingmodel.Finding) { r.findings.Report(finding) },
	})
	return r
}
func (rule *ruleExecutableBit) Name() string { return "executable-bit" }
func (rule *ruleExecutableBit) Description() string {
	return builtinRuleDescription("executable-bit")
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
	scope  Invocation
	parent *execution.ExecutableChecker
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
func (rule *ruleExecutableBit) EnterInvocation(scope Invocation) error {
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
