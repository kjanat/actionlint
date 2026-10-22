package eventpayload

import (
	"maps"
	"slices"
	"strings"

	expr "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/webhook/payload"
)

// Trigger identifies a possible event and optional activity filters. Unknown
// events/activities stay open, including reusable workflows' inherited events.
type Trigger struct {
	Name    string
	Actions []string
}

// FromTriggers returns a fresh projection owned by its caller. Optional and nullable
// members retain their known shape: Actions permits reading missing properties.
// Dynamic mappings remain open; only known object members are checked strictly.
func FromTriggers(triggers []Trigger) *expr.ObjectType {
	b := builder{cache: map[int]expr.Type{}, active: map[int]bool{}}
	var result expr.Type
	for _, trigger := range triggers {
		name := trigger.Name
		if name == "pull_request_target" {
			name = "pull_request"
		}
		variants, known := payload.Variants(name)
		if !known {
			return expr.NewEmptyObjectType()
		}
		actions := trigger.Actions
		if len(actions) == 0 {
			actions = slices.Sorted(maps.Keys(variants))
		}
		for _, action := range actions {
			id, ok := variants[action]
			if !ok {
				return expr.NewEmptyObjectType()
			}
			result = combine(result, b.build(id))
		}
	}
	if object, ok := result.(*expr.ObjectType); ok {
		return object
	}
	return expr.NewEmptyObjectType()
}

type builder struct {
	cache  map[int]expr.Type
	active map[int]bool
}

func (b *builder) build(id int) expr.Type {
	if t, ok := b.cache[id]; ok {
		return t
	}
	// Recursive payloads are finite at runtime but have unbounded schema depth.
	// Keep the recursive edge open instead of expanding or rejecting it.
	if b.active[id] {
		return expr.AnyType{}
	}
	b.active[id] = true
	defer delete(b.active, id)
	n := payload.Node(id)
	var t expr.Type
	switch n.Kind {
	case "absent", "null":
		return nil
	case "string":
		t = expr.StringType{}
	case "number":
		t = expr.NumberType{}
	case "boolean":
		t = expr.BoolType{}
	case "array":
		t = &expr.ArrayType{Elem: known(b.build(n.Element))}
	case "union":
		for _, alternative := range n.Alternatives {
			t = combine(t, b.build(alternative))
		}
	case "object":
		o := expr.NewEmptyStrictObjectType()
		o.DisplayName = "object"
		for _, name := range slices.Sorted(maps.Keys(n.Properties)) {
			o.Props[strings.ToLower(name)] = known(b.build(n.Properties[name].Type))
		}
		if n.Mapped != nil {
			o.Mapped = known(b.build(*n.Mapped))
		}
		t = o
	default:
		t = expr.AnyType{}
	}
	t = known(t)
	b.cache[id] = t
	return t
}

func known(t expr.Type) expr.Type {
	if t == nil {
		return expr.AnyType{}
	}
	return t
}

// combine preserves all properties offered by any possible event. Missing and
// null variants are not a reason to erase a known property's type.
func combine(a, b expr.Type) expr.Type {
	if a == nil {
		return b
	}
	if b == nil || a == b {
		return a
	}
	if x, ok := a.(*expr.ObjectType); ok {
		if y, ok := b.(*expr.ObjectType); ok {
			o := expr.NewStrictObjectType(maps.Clone(x.Props))
			o.DisplayName = "object"
			o.Mapped = combine(x.Mapped, y.Mapped)
			for name, t := range y.Props {
				o.Props[name] = combine(o.Props[name], t)
			}
			return o
		}
	}
	if x, ok := a.(*expr.ArrayType); ok {
		if y, ok := b.(*expr.ArrayType); ok {
			return &expr.ArrayType{Elem: combine(x.Elem, y.Elem)}
		}
	}
	switch a.(type) {
	case expr.StringType:
		if _, ok := b.(expr.StringType); ok {
			return a
		}
	case expr.NumberType:
		if _, ok := b.(expr.NumberType); ok {
			return a
		}
	case expr.BoolType:
		if _, ok := b.(expr.BoolType); ok {
			return a
		}
	}
	return expr.AnyType{}
}
