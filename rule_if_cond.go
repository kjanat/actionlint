package actionlint

import (
	"strings"
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
func (rule *RuleIfCond) VisitStep(n *Step) error {
	rule.checkIfCond(n.If)
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleIfCond) VisitJobPre(n *Job) error {
	rule.checkIfCond(n.If)
	if n.Snapshot != nil {
		rule.checkIfCond(n.Snapshot.If)
	}
	return nil
}

func (rule *RuleIfCond) checkIfCond(n *String) {
	if n == nil {
		return
	}
	expr, err, _ := parseConditionExpression(n.Value)
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
	checker := NewExprSemanticsChecker(false, nil)
	if !checker.IsConstant(expr) {
		return
	}
	if _, errs := checker.Check(expr); len(errs) != 0 {
		return
	}
	if value, known := conditionConstantValue(expr); known {
		i := strings.TrimSpace(conditionSource(n.Value))
		if strings.HasPrefix(i, "${{") {
			i = strings.TrimSpace(i[3 : len(i)-2])
		}
		if expressionTruthy(value) {
			rule.Errorf(n.Pos, "constant expression %q in condition is always truthy. remove the if: section", i)
		} else {
			rule.Errorf(n.Pos, "constant expression %q in condition is always falsy. this step or job will be skipped", i)
		}
	}
}
