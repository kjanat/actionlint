package builtin

import (
	"errors"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
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
func (rule *RuleShellName) VisitStep(view workflowdecl.StepView) error {
	if !view.Present() {
		return errors.New("shell-name step declaration is unavailable")
	}
	if run, ok := view.Run(); ok {
		rule.checkShellValue(run.Shell().Semantic())
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleShellName) VisitJobPre(view workflowdecl.JobView) error {
	if !view.Present() {
		return errors.New("shell-name job declaration is unavailable")
	}
	if rule.runners != nil {
		rule.platform = rule.runners.Job(view).Platform
	}
	rule.checkShellValue(execution.DefaultShell(view.Defaults().Run().Semantic()))
	return nil
}

// VisitJobPost is callback when visiting Job node after visiting its children.
func (rule *RuleShellName) VisitJobPost(workflowdecl.JobView) error {
	rule.platform = runnerlang.Any // Clear
	return nil
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleShellName) VisitWorkflowPre(view workflowdecl.WorkflowView) error {
	if !view.Present() {
		return errors.New("shell-name workflow declaration is unavailable")
	}
	rule.checkShellValue(execution.DefaultShell(view.Defaults().Run().Semantic()))
	return nil
}

func (rule *RuleShellName) checkShellValue(shell value.Value) {
	for _, d := range execution.CheckShell(shell, rule.platform) {
		rule.Report(findingmodel.FromDiagnostic(d))
	}
}
