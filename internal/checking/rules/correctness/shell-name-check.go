package correctness

import (
	"errors"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ruleShellName is a rule to check 'shell' field. For more details, see
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#using-a-specific-shell
type ruleShellName struct {
	runners *runnerlang.Knowledge
	workflowvisit.Base
	platform runnerlang.Platform
}

// NewRuleShellName checks run shells against known runner platforms.
// Nil runner knowledge accepts shells supported by any platform.
func NewRuleShellName(runners *runnerlang.Knowledge) workflowvisit.Rule {
	descriptor := ShellNameDeclaration()
	return &ruleShellName{
		Base:     workflowvisit.NewBase(descriptor.Name, descriptor.Description),
		runners:  runners,
		platform: runnerlang.Any,
	}
}

// VisitStep is callback when visiting Step node.
func (rule *ruleShellName) VisitStep(view workflowdecl.StepView) error {
	if !view.Present() {
		return errors.New("shell-name step declaration is unavailable")
	}
	if run, ok := view.Run(); ok {
		rule.checkShellValue(run.Shell().Semantic())
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *ruleShellName) VisitJobPre(view workflowdecl.JobView) error {
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
func (rule *ruleShellName) VisitJobPost(workflowdecl.JobView) error {
	rule.platform = runnerlang.Any // Clear
	return nil
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *ruleShellName) VisitWorkflowPre(view workflowdecl.WorkflowView) error {
	if !view.Present() {
		return errors.New("shell-name workflow declaration is unavailable")
	}
	rule.checkShellValue(execution.DefaultShell(view.Defaults().Run().Semantic()))
	return nil
}

func (rule *ruleShellName) checkShellValue(shell value.Value) {
	for _, d := range checkShell(shell, rule.platform) {
		rule.Report(findingmodel.FromDiagnostic(d))
	}
}
