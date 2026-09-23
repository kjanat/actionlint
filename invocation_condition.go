package actionlint

import "strings"

func invocationStepCondition(condition *String) (enabled, known bool) {
	if condition != nil {
		source := condition.Value
		if !condition.ContainsExpression() {
			source = "${{ " + source + " }}"
		}
		if conditionNeverRuns(parseAssignedExpression(source)) {
			return false, true
		}
	}
	return stepCondition(condition)
}

type conditionTruth uint8

const (
	conditionUnknown conditionTruth = iota
	conditionFalse
	conditionTrue
)

// conditionNeverRuns proves a step condition false in every possible status.
// success() and failure() inspect the same status, but cancelled() inspects the
// job even inside a composite action, where the other two inspect action_status.
// Keep cancellation independent and leave unsupported expressions unknown.
func conditionNeverRuns(expression ExprNode) bool {
	for _, status := range []string{"success", "failure", "other"} {
		for _, cancelled := range []bool{false, true} {
			if conditionStatusTruth(expression, status, cancelled) != conditionFalse {
				return false
			}
		}
	}
	return true
}

func conditionStatusTruth(expression ExprNode, status string, cancelled bool) conditionTruth {
	switch node := expression.(type) {
	case *NullNode:
		return conditionFalse
	case *BoolNode:
		return knownConditionTruth(node.Value)
	case *IntNode:
		return knownConditionTruth(node.Value != 0)
	case *FloatNode:
		return knownConditionTruth(node.Value != 0)
	case *StringNode:
		return knownConditionTruth(node.Value != "")
	case *FuncCallNode:
		if len(node.Args) != 0 {
			return conditionUnknown
		}
		switch strings.ToLower(node.Callee) {
		case "always":
			return conditionTrue
		case "success", "failure":
			return knownConditionTruth(strings.EqualFold(node.Callee, status))
		case "cancelled":
			return knownConditionTruth(cancelled)
		}
	case *NotOpNode:
		switch conditionStatusTruth(node.Operand, status, cancelled) {
		case conditionFalse:
			return conditionTrue
		case conditionTrue:
			return conditionFalse
		case conditionUnknown:
			return conditionUnknown
		}
	case *LogicalOpNode:
		left := conditionStatusTruth(node.Left, status, cancelled)
		right := conditionStatusTruth(node.Right, status, cancelled)
		switch node.Kind {
		case LogicalOpNodeKindAnd:
			if left == conditionFalse || right == conditionFalse {
				return conditionFalse
			}
			if left == conditionTrue && right == conditionTrue {
				return conditionTrue
			}
		case LogicalOpNodeKindOr:
			if left == conditionTrue || right == conditionTrue {
				return conditionTrue
			}
			if left == conditionFalse && right == conditionFalse {
				return conditionFalse
			}
		}
	}
	return conditionUnknown
}

func knownConditionTruth(value bool) conditionTruth {
	if value {
		return conditionTrue
	}
	return conditionFalse
}
