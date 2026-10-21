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

// NewEventType projects the properties accessible across possible events while
// retaining the alternatives for guarded expressions. Inputs are immutable.
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

// A missing member on one variant does not erase a known member on another:
// Actions permits reading absent properties. Conflicting types remain open.
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
	if len(selected) == len(event.EventVariants) || len(selected) == 0 {
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
		ref, literal := n.Left, n.Right
		if _, ok := ref.(*StringNode); ok {
			ref, literal = literal, ref
		}
		text, ok := literal.(*StringNode)
		if !ok {
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
			}
		default:
			return false, false
		}
		equal := strings.EqualFold(value, text.Value)
		return equal == (n.Kind == CompareOpNodeKindEq), true
	}
	return false, false
}
