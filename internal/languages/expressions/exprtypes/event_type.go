package exprtypes

import "maps"

// EventVariant with an empty event is unknown, and a strict payload can establish an absent action.
type EventVariant struct {
	Event, Action string
	Payload       ObjectType
}

// NewEventType returns a loose object without variants.
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
	return object.WithEventVariants(variants)
}

// Actions permits reading absent properties, and combineEventTypes keeps a
// member known in either variant.
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
