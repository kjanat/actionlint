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
	Payload       ObjectType
}

// NewEventType merges event payloads and attaches the variants for guard narrowing.
// Without variants it returns a loose object.
func NewEventType(variants []EventVariant) ObjectType {
	if len(variants) == 0 {
		return NewEmptyObjectType()
	}
	result := variants[0].Payload.Type()
	for _, variant := range variants[1:] {
		result = combineEventTypes(result, variant.Payload.Type())
	}
	object, ok := result.Object()
	if !ok {
		return NewEmptyObjectType()
	}
	d := object.rep()
	return ObjectType{&objectData{displayName: d.displayName, props: d.props, names: d.names, variants: slices.Clone(variants), mapped: d.mapped}}
}

// combineEventTypes joins the payload types of two variants. Actions permits
// reading absent properties, so a member known in one variant is included even
// if another omits it.
func combineEventTypes(a, b Type) Type {
	if a == b {
		return a
	}
	if x, ok := a.Object(); ok {
		if y, ok := b.Object(); ok {
			xd, yd := x.rep(), y.rep()
			props := maps.Clone(xd.props)
			if props == nil {
				props = map[string]Type{}
			}
			for name, t := range yd.props {
				if existing, ok := props[name]; ok {
					t = combineEventTypes(existing, t)
				}
				props[name] = t
			}
			// A missing mapped type means the side is strict; the other side's
			// mapped type, if any, is kept.
			mapped := xd.mapped
			if mapped == nil {
				mapped = yd.mapped
			} else if yd.mapped != nil {
				mapped = new(combineEventTypes(*xd.mapped, *yd.mapped))
			}
			return ObjectType{&objectData{displayName: "object", props: props, names: mergePropertyNames(xd, yd), mapped: mapped}}.Type()
		}
	}
	if x, ok := a.Array(); ok {
		if y, ok := b.Array(); ok {
			return NewArrayType(combineEventTypes(x.Elem(), y.Elem())).Type()
		}
	}
	return a.Merge(b)
}

// withEventGuard scopes narrowing to the evaluated branch without mutating a
// shared context, leaking assumptions to later expressions, or changing lookup.
func (sema *Checker) withEventGuard(guard Node, truth bool, check func() Type) Type {
	gh, ok := sema.vars["github"].Object()
	if !ok {
		return check()
	}
	event, ok := gh.Property("event")
	if !ok {
		return check()
	}
	payload, ok := event.Object()
	variants := payload.rep().variants
	if !ok || len(variants) == 0 {
		return check()
	}
	selected := make([]EventVariant, 0, len(variants))
	for _, variant := range variants {
		value, known := eventGuardValue(guard, variant)
		if !known || value == truth {
			selected = append(selected, variant)
		}
	}
	if len(selected) == 0 {
		// No configured trigger can evaluate this branch. Its payload accesses
		// and constant evaluation must not produce findings.
		return AnyType()
	}
	if len(selected) == len(variants) {
		return check()
	}
	previous := sema.vars
	sema.vars = maps.Clone(previous)
	sema.vars["github"] = gh.With("event", NewEventType(selected).Type()).Type()
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
		case LogicalOpNodeKindInvalid:
			// An invalid operator yields no guard.
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
				if variant.Event == "" || !variant.Payload.IsStrict() {
					return false, false
				}
				if _, exists := variant.Payload.Property("action"); exists {
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
