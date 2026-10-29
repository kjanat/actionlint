package builtin

import (
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

// RuleExecutableBit adapts workflow execution checks to traversal and composite scopes.
type RuleExecutableBit struct {
	*execution.ExecutableChecker
	ruleLogger
	frames   []executableInvocation
	findings []*findingmodel.Finding
}

func newRuleExecutableBit(c Context) *RuleExecutableBit {
	r := &RuleExecutableBit{tag: "executable-bit"}
	r.ExecutableChecker = execution.NewExecutableChecker(execution.ExecutableContext{
		Context: c.Context, ProjectRoot: c.ProjectRoot, WorkingDir: c.WorkingDir,
		Reader: c.Reader, Index: c.Index, Log: r.Debug, Runners: c.Runners,
	})
	return r
}
func (rule *RuleExecutableBit) Name() string { return "executable-bit" }
func (rule *RuleExecutableBit) Description() string {
	return builtinRuleBase("executable-bit").DescriptionText
}

func (rule *RuleExecutableBit) Errs() []*findingmodel.Finding                     { return rule.findings }
func (rule *RuleExecutableBit) VisitWorkflowPost(workflowdecl.WorkflowView) error { return nil }
func (rule *RuleExecutableBit) VisitJobPost(workflowdecl.JobView) error           { return nil }
func (rule *RuleExecutableBit) CheckoutPaths() *execution.Paths                   { return rule.Paths() }

type executableInvocation struct {
	scope  Invocation
	parent *execution.ExecutableChecker
}

func (rule *RuleExecutableBit) VisitStep(step workflowdecl.StepView) error {
	if _, action := step.Action(); action {
		return nil
	}
	return rule.VisitInvocationStep(step)
}
func (rule *RuleExecutableBit) VisitInvocationStep(step workflowdecl.StepView) error {
	before := len(rule.Findings())
	err := rule.ExecutableChecker.VisitStep(step)
	added := rule.Findings()[before:]
	rule.findings = append(rule.findings, added...)
	return err
}
func (rule *RuleExecutableBit) VisitOpaqueAction(step workflowdecl.StepView) error {
	return rule.VisitInvocationStep(step)
}
func (rule *RuleExecutableBit) EnterInvocation(scope Invocation) error {
	if !scope.Context.Valid() || scope.Source == nil {
		return invocation.ErrForeignContext
	}
	rule.frames = append(rule.frames, executableInvocation{scope: scope, parent: rule.ExecutableChecker})
	rule.ExecutableChecker = rule.ForkComposite(scope.Context, scope.ActionPath, *rule.Paths())
	return nil
}
func (rule *RuleExecutableBit) LeaveInvocation(context invocation.Context) error {
	if len(rule.frames) == 0 || rule.frames[len(rule.frames)-1].scope.Context != context {
		return invocation.ErrForeignContext
	}
	frame := rule.frames[len(rule.frames)-1]
	frame.parent.JoinComposite(context, rule.ExecutableChecker)
	rule.ExecutableChecker = frame.parent
	rule.frames = rule.frames[:len(rule.frames)-1]
	return nil
}
