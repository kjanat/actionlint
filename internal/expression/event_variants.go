package expression

import (
	"maps"
	"slices"
	"strings"
)

// EventVariant keeps an Actions trigger and activity correlated with its payload.
// An empty event is unknown; a strict payload can establish an absent action.
type EventVariant struct {
	Event, Action string
	Payload       *ObjectType
}

// NewEventType merges event payloads and attaches the variants for guard narrowing.
// It does not mutate the supplied payloads.
func NewEventType(variants []EventVariant) *ObjectType {
	var result Type
	for _, variant := range variants {
		result = combineEventTypes(result, variant.Payload)
	}
	if object, ok := result.(*ObjectType); ok {
		object = copyEventObject(object)
		object.EventVariants = slices.Clone(variants)
		return object
	}
	return NewEmptyObjectType()
}

// Actions permits reading absent properties, so a member known in one variant
// is included even if another omits it. Incompatible types merge to AnyType.
func combineEventTypes(a, b Type) Type {
	if a == nil {
		return b
	}
	if b == nil || a == b {
		return a
	}
	if x, ok := a.(*ObjectType); ok {
		if y, ok := b.(*ObjectType); ok {
			o := NewStrictObjectType(maps.Clone(x.Props))
			o.DisplayName = "object"
			o.PropertyNames = mergePropertyNames(x, y)
			o.Mapped = combineEventTypes(x.Mapped, y.Mapped)
			for name, t := range y.Props {
				o.Props[name] = combineEventTypes(o.Props[name], t)
			}
			return o
		}
	}
	if x, ok := a.(*ArrayType); ok {
		if y, ok := b.(*ArrayType); ok {
			return &ArrayType{Elem: combineEventTypes(x.Elem, y.Elem)}
		}
	}
	return a.Merge(b)
}

// withEventGuard scopes narrowing to the evaluated branch without mutating a
// shared context, leaking assumptions to later expressions, or changing lookup.
func (sema *Checker) withEventGuard(guard Node, truth bool, check func() Type) Type {
	gh, ok := sema.vars["github"].(*ObjectType)
	if !ok {
		return check()
	}
	event, ok := gh.Props["event"].(*ObjectType)
	if !ok || len(event.EventVariants) == 0 {
		return check()
	}
	selected := make([]EventVariant, 0, len(event.EventVariants))
	for _, variant := range event.EventVariants {
		value, known := eventGuardValue(guard, variant)
		if !known || value == truth {
			selected = append(selected, variant)
		}
	}
	if len(selected) == 0 {
		// No configured trigger can evaluate this branch. Its payload accesses
		// and constant evaluation must not produce findings.
		return AnyType{}
	}
	if len(selected) == len(event.EventVariants) {
		return check()
	}
	previous := sema.vars
	sema.vars = maps.Clone(previous)
	narrowed := copyEventObject(gh)
	narrowed.Props["event"] = NewEventType(selected)
	sema.vars["github"] = narrowed
	defer func() { sema.vars = previous }()
	return check()
}

func eventGuardValue(node Node, variant EventVariant) (bool, bool) {
	switch n := node.(type) {
	case *NotOpNode:
		value, known := eventGuardValue(n.Operand, variant)
		return !value, known
	case *LogicalOpNode:
		left, lk := eventGuardValue(n.Left, variant)
		right, rk := eventGuardValue(n.Right, variant)
		switch n.Kind {
		case LogicalOpNodeKindAnd:
			if lk && !left || rk && !right {
				return false, true
			}
			return left && right, lk && rk
		case LogicalOpNodeKindOr:
			if lk && left || rk && right {
				return true, true
			}
			return left || right, lk && rk
		}
	case *CompareOpNode:
		if n.Kind != CompareOpNodeKindEq && n.Kind != CompareOpNodeKindNotEq {
			return false, false
		}
		ref, operand := n.Left, n.Right
		constant, known := ConditionConstantValue(operand)
		if !known {
			ref, operand = operand, ref
			constant, known = ConditionConstantValue(operand)
		}
		text, ok := constant.(string)
		if !known || !ok {
			return false, false
		}
		var value string
		switch strings.ToLower(eventReference(ref)) {
		case "github.event_name":
			value = variant.Event
			if value == "" {
				return false, false
			}
		case "github.event.action":
			value = variant.Action
			if value == "" {
				if variant.Event == "" || variant.Payload == nil || !variant.Payload.IsStrict() {
					return false, false
				}
				if _, exists := variant.Payload.Props["action"]; exists {
					return false, false
				}
				return CompareConditionValues(n.Kind, nil, text)
			}
		default:
			return false, false
		}
		return CompareConditionValues(n.Kind, value, text)
	}
	return false, false
}
