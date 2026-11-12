package context

import (
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// NarrowEventTypes selects event payload variants that can reach the branch.
// Missing context data or unknown guards cannot eliminate a possible variant.
func NarrowEventTypes(guard exprsyntax.Node, truth bool, lookup func(string) (exprtypes.Type, bool)) (map[string]exprtypes.Type, bool) {
	current, _ := lookup("github")
	github, ok := current.Object()
	if !ok {
		return nil, true
	}
	event, ok := github.Property("event")
	if !ok {
		return nil, true
	}
	payload, ok := event.Object()
	if !ok {
		return nil, true
	}
	variants := payload.EventVariants()
	if len(variants) == 0 {
		return nil, true
	}
	selected := make([]exprtypes.EventVariant, 0, len(variants))
	for _, variant := range variants {
		value, known := eventGuardValue(guard, variant)
		if !known || value == truth {
			selected = append(selected, variant)
		}
	}
	if len(selected) == 0 {
		return nil, false
	}
	if len(selected) == len(variants) {
		return nil, true
	}
	return map[string]exprtypes.Type{"github": github.With("event", exprtypes.NewEventType(selected).Type()).Type()}, true
}

func eventGuardValue(node exprsyntax.Node, variant exprtypes.EventVariant) (bool, bool) {
	switch n := node.(type) {
	case *exprsyntax.NotOpNode:
		value, known := eventGuardValue(n.Operand, variant)
		return !value, known
	case *exprsyntax.LogicalOpNode:
		left, lk := eventGuardValue(n.Left, variant)
		right, rk := eventGuardValue(n.Right, variant)
		switch n.Kind {
		case exprsyntax.LogicalOpNodeKindAnd:
			if lk && !left || rk && !right {
				return false, true
			}
			return left && right, lk && rk
		case exprsyntax.LogicalOpNodeKindOr:
			if lk && left || rk && right {
				return true, true
			}
			return left || right, lk && rk
		case exprsyntax.LogicalOpNodeKindInvalid:
			// An invalid operator yields no guard.
		}
	case *exprsyntax.CompareOpNode:
		if n.Kind != exprsyntax.CompareOpNodeKindEq && n.Kind != exprsyntax.CompareOpNodeKindNotEq {
			return false, false
		}
		ref, operand := n.Left, n.Right
		constant, known := expreval.ConditionConstantValue(operand)
		if !known {
			ref, operand = operand, ref
			constant, known = expreval.ConditionConstantValue(operand)
		}
		text, ok := constant.(string)
		if !known || !ok {
			return false, false
		}
		var value string
		switch strings.ToLower(exprsyntax.DisplayReference(ref)) {
		case "github.event_name":
			value = variant.Event
			if value == "" {
				return false, false
			}
		case "github.event.action":
			value = variant.Action
			if value == "" {
				if variant.Event == "" || !variant.Payload.IsStrict() {
					return false, false
				}
				if _, exists := variant.Payload.Property("action"); exists {
					return false, false
				}
				return exprvalues.CompareConditionValues(n.Kind, nil, text)
			}
		default:
			return false, false
		}
		return exprvalues.CompareConditionValues(n.Kind, value, text)
	}
	return false, false
}
