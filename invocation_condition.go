package actionlint

import "strings"

func invocationCondition(condition *String) (enabled, known bool) {
	return invocationConditionInStatuses(condition, []string{"success", "failure", "other"})
}

func jobInvocationCondition(job *Job) (enabled, known bool) {
	if len(job.Needs) == 0 {
		// No ancestor can have failed. Cancellation still prevents assuming success.
		return invocationConditionInStatuses(job.If, []string{"success", "other"})
	}
	return invocationCondition(job.If)
}

func invocationConditionInStatuses(condition *String, statuses []string) (enabled, known bool) {
	if condition != nil {
		expr, _, _ := parseConditionExpression(condition.Value)
		if conditionNeverRunsInStatuses(expr, statuses) {
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

// conditionNeverRuns proves a job or step condition false in every possible status.
// For jobs, success requires a successful dependency chain, excluding failure in
// that chain. For steps, success() and failure() inspect the same execution status.
// cancelled() inspects the job even inside composites, whose other two status
// functions inspect action_status instead.
// Keep cancellation independent and leave unsupported expressions unknown.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idneeds
func conditionNeverRuns(expression ExprNode) bool {
	return conditionNeverRunsInStatuses(expression, []string{"success", "failure", "other"})
}

func conditionNeverRunsInStatuses(expression ExprNode, statuses []string) bool {
	for _, status := range statuses {
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
