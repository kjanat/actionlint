package cachepolicy

import (
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// cacheReachableEvents removes only events for which the job condition is false.
// Other context values and unsupported expressions remain unknown.
func cacheReachableEvents(events []string, job *workflowlang.Job) []string {
	if job.If == nil {
		return events
	}
	expr, err, _ := exprlang.ParseConditionExpression(job.If.Value)
	if err != nil || expr == nil {
		return events
	}
	reachable := make([]string, 0, len(events))
	for _, event := range events {
		value, known := cacheEventCondition(expr, event)
		if !known || value {
			reachable = append(reachable, event)
		}
	}
	return reachable
}

func cacheEventCondition(expr exprlang.Node, event string) (bool, bool) {
	switch n := expr.(type) {
	case *exprlang.NotOpNode:
		value, known := cacheEventCondition(n.Operand, event)
		return !value, known
	case *exprlang.LogicalOpNode:
		left, lok := cacheEventCondition(n.Left, event)
		right, rok := cacheEventCondition(n.Right, event)
		if n.Kind == exprlang.LogicalOpNodeKindAnd {
			if (lok && !left) || (rok && !right) {
				return false, true
			}
			return left && right, lok && rok
		}
		if (lok && left) || (rok && right) {
			return true, true
		}
		return false, lok && rok
	default:
		value, known := cacheEventValue(expr, event)
		return exprlang.ExpressionTruthy(value), known
	}
}

func cacheEventValue(expr exprlang.Node, event string) (any, bool) {
	switch n := expr.(type) {
	case *exprlang.ObjectDerefNode:
		if receiver, ok := n.Receiver.(*exprlang.VariableNode); ok && strings.EqualFold(receiver.Name, "github") && strings.EqualFold(n.Property, "event_name") {
			return event, true
		}
	case *exprlang.IndexAccessNode:
		if receiver, ok := n.Operand.(*exprlang.VariableNode); ok && strings.EqualFold(receiver.Name, "github") {
			if key, ok := n.Index.(*exprlang.StringNode); ok && strings.EqualFold(key.Value, "event_name") {
				return event, true
			}
		}
	case *exprlang.CompareOpNode:
		left, lok := cacheEventValue(n.Left, event)
		right, rok := cacheEventValue(n.Right, event)
		if lok && rok {
			return exprlang.CompareConditionValues(n.Kind, left, right)
		}
		return nil, false
	}
	return exprlang.ConditionConstantValue(expr)
}
