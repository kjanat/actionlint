package cachepolicy

import (
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// cacheReachableEvents removes only events for which the job condition is false.
func cacheReachableEvents(events []string, condition string) []string {
	if condition == "" {
		return events
	}
	expr, err, _ := exprsyntax.ParseConditionExpression(condition)
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

func cacheEventCondition(expr exprsyntax.Node, event string) (bool, bool) {
	switch n := expr.(type) {
	case *exprsyntax.NotOpNode:
		value, known := cacheEventCondition(n.Operand, event)
		return !value, known
	case *exprsyntax.LogicalOpNode:
		left, lok := cacheEventCondition(n.Left, event)
		right, rok := cacheEventCondition(n.Right, event)
		if n.Kind == exprsyntax.LogicalOpNodeKindAnd {
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
		return exprvalues.ExpressionTruthy(value), known
	}
}

func cacheEventValue(expr exprsyntax.Node, event string) (any, bool) {
	switch n := expr.(type) {
	case *exprsyntax.ObjectDerefNode:
		if receiver, ok := n.Receiver.(*exprsyntax.VariableNode); ok && strings.EqualFold(receiver.Name, "github") && strings.EqualFold(n.Property, "event_name") {
			return event, true
		}
	case *exprsyntax.IndexAccessNode:
		if receiver, ok := n.Operand.(*exprsyntax.VariableNode); ok && strings.EqualFold(receiver.Name, "github") {
			if key, ok := n.Index.(*exprsyntax.StringNode); ok && strings.EqualFold(key.Value, "event_name") {
				return event, true
			}
		}
	case *exprsyntax.CompareOpNode:
		left, lok := cacheEventValue(n.Left, event)
		right, rok := cacheEventValue(n.Right, event)
		if lok && rok {
			return exprvalues.CompareConditionValues(n.Kind, left, right)
		}
		return nil, false
	}
	return expreval.ConditionConstantValue(expr)
}
