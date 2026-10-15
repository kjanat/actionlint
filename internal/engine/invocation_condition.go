package engine

import (
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func invocationCondition(condition *workflowlang.String) (enabled, known bool) {
	return invocationConditionInStatuses(condition, []string{"success", "failure", "other"})
}

func jobInvocationCondition(job *workflowlang.Job) (enabled, known bool) {
	if len(job.Needs) == 0 {
		// No ancestor can have failed. Cancellation still prevents assuming success.
		return invocationConditionInStatuses(job.If, []string{"success", "other"})
	}
	return invocationCondition(job.If)
}

func invocationConditionInStatuses(condition *workflowlang.String, statuses []string) (enabled, known bool) {
	if condition != nil {
		expr, _, _ := exprlang.ParseConditionExpression(condition.Value)
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
func conditionNeverRuns(expression exprlang.Node) bool {
	return conditionNeverRunsInStatuses(expression, []string{"success", "failure", "other"})
}

func conditionNeverRunsInStatuses(expression exprlang.Node, statuses []string) bool {
	for _, status := range statuses {
		for _, cancelled := range []bool{false, true} {
			if conditionStatusTruth(expression, status, cancelled) != conditionFalse {
				return false
			}
		}
	}
	return true
}

func conditionStatusTruth(expression exprlang.Node, status string, cancelled bool) conditionTruth {
	switch node := expression.(type) {
	case *exprlang.NullNode:
		return conditionFalse
	case *exprlang.BoolNode:
		return knownConditionTruth(node.Value)
	case *exprlang.IntNode:
		return knownConditionTruth(node.Value != 0)
	case *exprlang.FloatNode:
		return knownConditionTruth(node.Value != 0)
	case *exprlang.StringNode:
		return knownConditionTruth(node.Value != "")
	case *exprlang.FuncCallNode:
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
	case *exprlang.NotOpNode:
		switch conditionStatusTruth(node.Operand, status, cancelled) {
		case conditionFalse:
			return conditionTrue
		case conditionTrue:
			return conditionFalse
		case conditionUnknown:
			return conditionUnknown
		}
	case *exprlang.LogicalOpNode:
		left := conditionStatusTruth(node.Left, status, cancelled)
		right := conditionStatusTruth(node.Right, status, cancelled)
		switch node.Kind {
		case exprlang.LogicalOpNodeKindAnd:
			if left == conditionFalse || right == conditionFalse {
				return conditionFalse
			}
			if left == conditionTrue && right == conditionTrue {
				return conditionTrue
			}
		case exprlang.LogicalOpNodeKindOr:
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
