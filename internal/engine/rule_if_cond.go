package engine

import (
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
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
	expr, err, _ := exprlang.ParseConditionExpression(n.Value)
	if err != nil {
		return // The expression rule reports parse errors.
	}
	if expr == nil {
		rule.Errorf(
			n.Pos,
			"if: condition %q is always evaluated to true because extra characters are around ${{ }}",
			n.Value,
		)
		return
	}
	checker := exprlang.NewChecker(false, nil)
	if !checker.IsConstant(expr) {
		return
	}
	if _, errs := checker.Check(expr); len(errs) != 0 {
		return
	}
	if value, known := exprlang.ConditionConstantValue(expr); known {
		i := strings.TrimSpace(exprlang.ConditionSource(n.Value))
		if strings.HasPrefix(i, "${{") {
			i = strings.TrimSpace(i[3 : len(i)-2])
		}
		if exprlang.ExpressionTruthy(value) {
			rule.Errorf(n.Pos, "constant expression %q in condition is always truthy. remove the if: section", i)
		} else {
			rule.Errorf(n.Pos, "constant expression %q in condition is always falsy. this step or job will be skipped", i)
		}
	}
}
