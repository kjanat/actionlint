package builtin

import (
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"context"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// RuleExecutableBit adapts workflow execution checks to traversal and composite scopes.
type RuleExecutableBit struct {
	*execution.ExecutableChecker
	ruleLogger
}

func newRuleExecutableBit(c Context) *RuleExecutableBit {
	ctx := c.Context
	if ctx == nil {
		ctx = context.Background()
	}
	r := &RuleExecutableBit{tag: "executable-bit"}
	r.ExecutableChecker = execution.NewExecutableChecker(execution.ExecutableContext{
		Context: ctx, ProjectRoot: c.ProjectRoot, WorkingDir: c.WorkingDir,
		Reader: c.Reader, Index: c.Index, Log: r.Debug, Runners: c.Runners,
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
func (rule *RuleExecutableBit) CheckoutPaths() *execution.Paths                { return rule.Paths() }
func (rule *RuleExecutableBit) DeferActionInvocation() bool                    { return true }
func (rule *RuleExecutableBit) ForComposite(invocation Invocation) (workflowvisit.Rule, error) {
	paths := *rule.Paths()
	paths = invocation.Actions.ApplyCheckout(paths)
	return &RuleExecutableBit{ExecutableChecker: rule.ForkComposite(invocation.Context, invocation.ActionPath, paths)}, nil
}

func (rule *RuleExecutableBit) JoinCompositeResult(context invocation.Context, result workflowvisit.Rule) {
	if child, ok := result.(*RuleExecutableBit); ok {
		rule.JoinComposite(context, child.ExecutableChecker)
	}
}
