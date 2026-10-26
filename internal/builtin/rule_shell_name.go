package builtin

import (
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"errors"
)

// RuleShellName is a rule to check 'shell' field. For more details, see
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#using-a-specific-shell
type RuleShellName struct {
	runners *runnerlang.Knowledge
	workflowvisit.Base
	platform runnerlang.Platform
	source   *documentparse.SourceSnapshot
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
	if rule.source == nil {
		return errors.New("shell-name source snapshot is unavailable")
	}
	view, err := rule.source.StepView(n)
	if err != nil {
		return err
	}
	if run, ok := view.Run(); ok {
		rule.checkShellValue(run.Shell().Semantic())
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleShellName) VisitJobPre(n *workflowlang.Job) error {
	if rule.source == nil {
		return errors.New("shell-name source snapshot is unavailable")
	}
	view, err := rule.source.JobView(n)
	if err != nil {
		return err
	}
	if rule.runners != nil {
		rule.platform = rule.runners.Job(view).Platform
	}
	rule.checkShellValue(execution.DefaultShell(view.Defaults().Run().Semantic()))
	return nil
}

// VisitJobPost is callback when visiting Job node after visiting its children.
func (rule *RuleShellName) VisitJobPost(n *workflowlang.Job) error {
	rule.platform = runnerlang.Any // Clear
	return nil
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleShellName) VisitWorkflowPre(n *workflowlang.Workflow) error {
	if rule.source == nil {
		return errors.New("shell-name source snapshot is unavailable")
	}
	view, present := rule.source.WorkflowView()
	if !present {
		return errors.New("shell-name workflow declaration is unavailable")
	}
	rule.checkShellValue(execution.DefaultShell(view.Defaults().Run().Semantic()))
	return nil
}

func (rule *RuleShellName) checkShellValue(shell value.Value) {
	for _, d := range execution.CheckShell(shell, rule.platform) {
		rule.Findings = append(rule.Findings, findingmodel.FromDiagnostic(d))
	}
}
