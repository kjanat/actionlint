package contexts

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// NarrowEventTypes selects event payload variants that can reach the branch.
// Missing context data or unknown guards cannot eliminate a possible variant.
func NarrowEventTypes(guard syntax.Node, truth bool, lookup func(string) (types.Type, bool)) (map[string]types.Type, bool) {
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
	selected := make([]types.EventVariant, 0, len(variants))
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
	return map[string]types.Type{"github": github.With("event", types.NewEventType(selected).Type()).Type()}, true
}

func eventGuardValue(node syntax.Node, variant types.EventVariant) (bool, bool) {
	switch n := node.(type) {
	case *syntax.NotOpNode:
		value, known := eventGuardValue(n.Operand, variant)
		return !value, known
	case *syntax.LogicalOpNode:
		left, lk := eventGuardValue(n.Left, variant)
		right, rk := eventGuardValue(n.Right, variant)
		switch n.Kind {
		case syntax.LogicalOpNodeKindAnd:
			if lk && !left || rk && !right {
				return false, true
			}
			return left && right, lk && rk
		case syntax.LogicalOpNodeKindOr:
			if lk && left || rk && right {
				return true, true
			}
			return left || right, lk && rk
		case syntax.LogicalOpNodeKindInvalid:
			// An invalid operator yields no guard.
		}
	case *syntax.CompareOpNode:
		if n.Kind != syntax.CompareOpNodeKindEq && n.Kind != syntax.CompareOpNodeKindNotEq {
			return false, false
		}
		ref, operand := n.Left, n.Right
		constant, known := evaluate.ConditionConstantValue(operand)
		if !known {
			ref, operand = operand, ref
			constant, known = evaluate.ConditionConstantValue(operand)
		}
		text, ok := constant.(string)
		if !known || !ok {
			return false, false
		}
		var value string
		switch strings.ToLower(syntax.DisplayReference(ref)) {
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
				return values.CompareConditionValues(n.Kind, nil, text)
			}
		default:
			return false, false
		}
		return values.CompareConditionValues(n.Kind, value, text)
	}
	return false, false
}
