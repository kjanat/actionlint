package typecheck

import (
	"maps"
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// withEventGuard scopes narrowing to the evaluated branch without mutating a
// shared context, leaking assumptions to later expressions, or changing lookup.
func (sema *Checker) withEventGuard(guard exprsyntax.Node, truth bool, check func() exprtypes.Type) exprtypes.Type {
	gh, ok := sema.vars["github"].Object()
	if !ok {
		return check()
	}
	event, ok := gh.Property("event")
	if !ok {
		return check()
	}
	payload, ok := event.Object()
	variants := payload.EventVariants()
	if !ok || len(variants) == 0 {
		return check()
	}
	selected := make([]exprtypes.EventVariant, 0, len(variants))
	for _, variant := range variants {
		value, known := eventGuardValue(guard, variant)
		if !known || value == truth {
			selected = append(selected, variant)
		}
	}
	if len(selected) == 0 {
		// No configured trigger can evaluate this branch. Its payload accesses
		// and constant evaluation must not produce findings.
		return exprtypes.AnyType()
	}
	if len(selected) == len(variants) {
		return check()
	}
	previous := sema.vars
	sema.vars = maps.Clone(previous)
	sema.vars["github"] = gh.With("event", exprtypes.NewEventType(selected).Type()).Type()
	defer func() { sema.vars = previous }()
	return check()
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
