package engine

import (
	"actionlint.kjanat.dev/internal/checks/condition"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// RuleIfCond is a rule to check if: conditions.
type RuleIfCond struct {
	RuleBase
}

// NewRuleIfCond creates new RuleIfCond instance.
func NewRuleIfCond() *RuleIfCond {
	return &RuleIfCond{
		RuleBase: builtinRuleBase("if-cond"),
	}
}

// VisitStep is callback when visiting Step node.
func (rule *RuleIfCond) VisitStep(n *workflowlang.Step) error {
	rule.checkIfCond(n.If)
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleIfCond) VisitJobPre(n *workflowlang.Job) error {
	rule.checkIfCond(n.If)
	if n.Snapshot != nil {
		rule.checkIfCond(n.Snapshot.If)
	}
	return nil
}

func (rule *RuleIfCond) checkIfCond(n *workflowlang.String) {
	if n == nil {
		return
	}
	for _, finding := range condition.Check(n.Value, factPosition(n.Pos)) {
		rule.errs = append(rule.errs, legacyDiagnosticError(finding))
	}
}
