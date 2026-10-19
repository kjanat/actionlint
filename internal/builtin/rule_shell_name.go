package builtin

import (
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	runnerlang "actionlint.kjanat.dev/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// RuleShellName is a rule to check 'shell' field. For more details, see
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#using-a-specific-shell
type RuleShellName struct {
	runners *runnerlang.Knowledge
	workflowvisit.Base
	platform runnerlang.Platform
}

// NewRuleShellName creates new RuleShellName instance.
func NewRuleShellName() *RuleShellName {
	return &RuleShellName{
		Base:     builtinRuleBase("shell-name"),
		platform: runnerlang.Any,
	}
}

// VisitStep is callback when visiting Step node.
func (rule *RuleShellName) VisitStep(n *workflowlang.Step) error {
	if run, ok := n.Exec.(*workflowlang.ExecRun); ok {
		rule.checkShellName(run.Shell)
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleShellName) VisitJobPre(n *workflowlang.Job) error {
	if n.RunsOn == nil {
		return nil
	}
	rule.platform = rule.runners.Job(n).Platform
	rule.checkShellValue(execution.DefaultShell(n.Defaults))
	return nil
}

// VisitJobPost is callback when visiting Job node after visiting its children.
func (rule *RuleShellName) VisitJobPost(n *workflowlang.Job) error {
	rule.platform = runnerlang.Any // Clear
	return nil
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleShellName) VisitWorkflowPre(n *workflowlang.Workflow) error {
	rule.checkShellValue(execution.DefaultShell(n.Defaults))
	return nil
}

func (rule *RuleShellName) checkShellName(node *workflowlang.String) {
	rule.checkShellValue(execution.ShellValueOf(node))
}

func (rule *RuleShellName) checkShellValue(shell execution.ShellValue) {
	for _, d := range execution.CheckShell(shell, rule.platform) {
		rule.Findings = append(rule.Findings, findingmodel.FromDiagnostic(d))
	}
}
