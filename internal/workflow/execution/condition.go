package execution

import (
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func Condition(condition value.Value) (enabled, known bool) {
	return invocationConditionInStatuses(condition, []string{"success", "failure", "other"})
}

func JobCondition(condition value.Value, hasNeeds bool) (enabled, known bool) {
	if !hasNeeds {
		// No ancestor can have failed. Cancellation still prevents assuming success.
		return invocationConditionInStatuses(condition, []string{"success", "other"})
	}
	return Condition(condition)
}

func invocationConditionInStatuses(condition value.Value, statuses []string) (enabled, known bool) {
	if condition.State() == value.Dynamic {
		expr, parseErr, _ := exprlang.ParseConditionExpression(condition.Source())
		if parseErr != nil {
			return false, false
		}
		if conditionNeverRunsInStatuses(expr, statuses) {
			return false, true
		}
	}
	return StepCondition(condition)
}

type conditionTruth uint8

const (
	conditionUnknown conditionTruth = iota
	conditionFalse
	conditionTrue
)

// NeverRuns proves a job or step condition false in every possible status.
// For jobs, success requires a successful dependency chain, excluding failure in
// that chain. For steps, success() and failure() inspect the same execution status.
// cancelled() inspects the job even inside composites, whose other two status
// functions inspect action_status instead.
// Keep cancellation independent and leave unsupported expressions unknown.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idneeds
func NeverRuns(expression exprlang.Node) bool {
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

func StepCondition(condition value.Value) (enabled, known bool) {
	if condition.State() == value.Missing {
		return true, true
	}
	if condition.Known() {
		return SemanticTruth(condition)
	}
	if condition.State() != value.Dynamic {
		return false, false
	}
	expression, parseErr, _ := exprlang.ParseConditionExpression(condition.Source())
	if parseErr != nil {
		return false, false
	}
	constant, known := exprlang.ConditionConstantValue(expression)
	if !known {
		// Explicit success() has the same gate as an ordinary step.
		if call, ok := expression.(*exprlang.FuncCallNode); ok && strings.EqualFold(call.Callee, "success") && len(call.Args) == 0 {
			return true, true
		}
		return false, false
	}
	switch value := constant.(type) {
	case nil:
		return false, true
	case bool:
		return value, true
	case float64:
		return value != 0, true
	case string:
		return value != "", true
	default:
		// Known JSON arrays and objects are truthy, including empty ones.
		return true, true
	}
}

func SemanticTruth(semantic value.Value) (enabled, known bool) {
	if !semantic.Known() {
		return false, false
	}
	switch semantic.Kind() {
	case value.Null:
		return false, true
	case value.Bool:
		result, _ := semantic.Bool()
		return result, true
	case value.Number:
		result, _ := semantic.Number()
		return result != 0, true
	case value.String:
		result, _ := semantic.Text()
		return result != "", true
	case value.Array, value.Object:
		return true, true
	default:
		return false, false
	}
}

func CanRunAfterFailure(condition value.Value) bool {
	if condition.State() == value.Missing {
		return false
	}
	if condition.State() == value.Invalid {
		return true
	}
	if condition.State() != value.Dynamic {
		return false
	}
	expression, err, _ := exprlang.ParseConditionExpression(condition.Source())
	if err != nil || expression == nil {
		return true
	}
	hasStatusFunction := false
	exprlang.Visit(expression, func(node, _ exprlang.Node, entering bool) {
		if call, ok := node.(*exprlang.FuncCallNode); entering && ok {
			switch strings.ToLower(call.Callee) {
			case "always", "cancelled", "failure", "success":
				hasStatusFunction = true
			}
		}
	})
	// A status function removes the runner's implicit success() condition.
	return hasStatusFunction && !expressionRequiresSuccess(expression)
}

func expressionRequiresSuccess(expression exprlang.Node) bool {
	switch node := expression.(type) {
	case *exprlang.FuncCallNode:
		return strings.EqualFold(node.Callee, "success") && len(node.Args) == 0
	case *exprlang.LogicalOpNode:
		left, right := expressionRequiresSuccess(node.Left), expressionRequiresSuccess(node.Right)
		switch node.Kind {
		case exprlang.LogicalOpNodeKindAnd:
			return left || right
		case exprlang.LogicalOpNodeKindOr:
			return left && right
		default:
			return false
		}
	default:
		return false
	}
}

func BoolMayBeTrue(semantic value.Value) bool {
	if semantic.State() == value.Missing {
		return false
	}
	if enabled, known := semantic.Bool(); known {
		return enabled
	}
	return true
}

// A known self checkout establishes which index is represented in the workspace.
// Opaque actions may change permissions or replace files, so invalidate that state.
