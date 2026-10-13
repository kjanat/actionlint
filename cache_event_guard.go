package actionlint

import "strings"

// cacheReachableEvents removes only events for which the job condition is false.
// Other context values and unsupported expressions remain unknown.
func cacheReachableEvents(events []string, job *Job) []string {
	if job.If == nil {
		return events
	}
	expr, err, _ := parseConditionExpression(job.If.Value)
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

func cacheEventCondition(expr ExprNode, event string) (bool, bool) {
	switch n := expr.(type) {
	case *NotOpNode:
		value, known := cacheEventCondition(n.Operand, event)
		return !value, known
	case *LogicalOpNode:
		left, lok := cacheEventCondition(n.Left, event)
		right, rok := cacheEventCondition(n.Right, event)
		if n.Kind == LogicalOpNodeKindAnd {
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
		return expressionTruthy(value), known
	}
}

func cacheEventValue(expr ExprNode, event string) (any, bool) {
	switch n := expr.(type) {
	case *ObjectDerefNode:
		if receiver, ok := n.Receiver.(*VariableNode); ok && strings.EqualFold(receiver.Name, "github") && strings.EqualFold(n.Property, "event_name") {
			return event, true
		}
	case *IndexAccessNode:
		if receiver, ok := n.Operand.(*VariableNode); ok && strings.EqualFold(receiver.Name, "github") {
			if key, ok := n.Index.(*StringNode); ok && strings.EqualFold(key.Value, "event_name") {
				return event, true
			}
		}
	case *CompareOpNode:
		left, lok := cacheEventValue(n.Left, event)
		right, rok := cacheEventValue(n.Right, event)
		if lok && rok {
			return compareConditionValues(n.Kind, left, right)
		}
		return nil, false
	}
	return conditionConstantValue(expr)
}
