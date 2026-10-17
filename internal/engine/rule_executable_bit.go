package engine

import (
	"context"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// RuleExecutableBit adapts workflow execution checks to traversal and composite scopes.
type RuleExecutableBit struct {
	*execution.ExecutableChecker
	ruleLogger
}

func newRuleExecutableBit(c ruleContext) *RuleExecutableBit {
	ctx := context.Background()
	if c.process != nil {
		ctx = c.process.Ctx
	}
	r := &RuleExecutableBit{tag: "executable-bit"}
	r.ExecutableChecker = execution.NewExecutableChecker(execution.ExecutableContext{
		Context: ctx, ProjectRoot: c.projectRoot, WorkingDir: c.workingDir,
		Reader: c.reader, Index: c.gitModes.load, Log: r.Debug,
	})
	return r
}
func (rule *RuleExecutableBit) Name() string { return "executable-bit" }
func (rule *RuleExecutableBit) Description() string {
	return builtinRuleBase("executable-bit").DescriptionText
}
func (rule *RuleExecutableBit) Errs() []*findingmodel.Finding                  { return rule.Findings() }
func (rule *RuleExecutableBit) VisitWorkflowPost(*workflowlang.Workflow) error { return nil }
func (rule *RuleExecutableBit) VisitJobPost(*workflowlang.Job) error           { return nil }
func (rule *RuleExecutableBit) checkoutPaths() *execution.Paths                { return rule.Paths() }
func (rule *RuleExecutableBit) deferActionInvocation() bool                    { return true }
func (rule *RuleExecutableBit) forComposite(invocation compositeInvocation) (workflowvisit.Rule, error) {
	paths := *rule.Paths()
	compositeCheckoutScope(&paths, invocation.actions)
	return &RuleExecutableBit{ExecutableChecker: rule.ForkComposite(invocation.call, invocation.actionPath, paths)}, nil
}
func (rule *RuleExecutableBit) joinCompositeResult(call *workflowlang.Step, result workflowvisit.Rule) {
	if child, ok := result.(*RuleExecutableBit); ok {
		rule.JoinComposite(call, child.ExecutableChecker)
	}
}
