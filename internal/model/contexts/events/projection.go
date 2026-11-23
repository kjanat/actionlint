package events

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
)

type Trigger struct {
	Name    string
	Actions []string
}

func FromTriggers(triggers []Trigger) exprtypes.ObjectType {
	b := builder{cache: map[int]exprtypes.Type{}, active: map[int]bool{}}
	var selected []exprtypes.EventVariant
	for _, trigger := range triggers {
		name := trigger.Name
		if name == "pull_request_target" {
			name = "pull_request"
		}
		variants, known := PayloadVariants(name)
		var dispatch exprtypes.ObjectType
		hasDispatch := false
		if trigger.Name == "repository_dispatch" {
			// OpenAPI indexes this shared schema by an example event_type.
			var combined member
			for _, action := range slices.Sorted(maps.Keys(variants)) {
				combined = combine(combined, b.build(variants[action]))
			}
			if object, ok := combined.ty.Object(); ok {
				dispatch = object.With("client_payload", exprtypes.NewEmptyObjectType().Type())
				hasDispatch = true
			}
		}
		actions := trigger.Actions
		if len(actions) == 0 {
			actions, _ = DefaultActivities(trigger.Name)
		}
		if len(actions) == 0 {
			// Events without a types filter can still carry webhook actions, such as
			// deployment's "created" payload.
			if trigger.Name != "repository_dispatch" {
				actions = slices.Sorted(maps.Keys(variants))
			}
			if len(actions) == 0 {
				actions = []string{""}
			}
		}
		for _, action := range actions {
			id, ok := variants[action]
			object := exprtypes.NewEmptyObjectType()
			if hasDispatch {
				object = dispatch
			} else if known && ok {
				if projected, ok := b.build(id).ty.Object(); ok {
					object = projected
				}
			}
			if trigger.Name == "workflow_run" {
				object = workflowRunPayload(object)
			}
			event := trigger.Name
			if event == "workflow_call" {
				// A reusable workflow inherits its caller's actual event name.
				event = ""
			}
			selected = append(selected, exprtypes.EventVariant{Event: event, Action: action, Payload: object})
		}
	}
	return exprtypes.NewEventType(selected)
}

func workflowRunPayload(object exprtypes.ObjectType) exprtypes.ObjectType {
	runType, _ := object.Property("workflow_run")
	run, ok := runType.Object()
	if !ok {
		return object
	}
	repositoryType, _ := run.Property("head_repository")
	repository, ok := repositoryType.Object()
	if !ok {
		return object
	}
	repository = repository.With("homepage", exprtypes.StringType())
	run = run.With("head_repository", repository.Type())
	return object.With("workflow_run", run.Type())
}

type member struct {
	ty      exprtypes.Type
	present bool
}

func present(t exprtypes.Type) member { return member{t, true} }

type builder struct {
	cache  map[int]exprtypes.Type
	active map[int]bool
}

func (b *builder) build(id int) member {
	if t, ok := b.cache[id]; ok {
		return present(t)
	}
	// Recursive payloads are finite at runtime but have unbounded schema depth.
	if b.active[id] {
		return present(exprtypes.AnyType())
	}
	b.active[id] = true
	defer delete(b.active, id)
	n := PayloadNode(id)
	var m member
	switch n.Kind {
	case "absent", "null":
		return member{}
	case "string":
		m = present(exprtypes.StringType())
	case "number":
		m = present(exprtypes.NumberType())
	case "boolean":
		m = present(exprtypes.BoolType())
	case "array":
		m = present(exprtypes.NewArrayType(known(b.build(n.Element))).Type())
	case "union", "intersection":
		for _, alternative := range n.Alternatives {
			m = combine(m, b.build(alternative))
		}
	case "object":
		var o exprtypes.ObjectBuilder
		o.SetDisplayName("object")
		for _, name := range slices.Sorted(maps.Keys(n.Properties)) {
			o.SetProperty(name, known(b.build(n.Properties[name].Type)))
		}
		if n.Mapped != nil {
			o.SetMapped(known(b.build(*n.Mapped)))
		}
		m = present(o.Build().Type())
	default:
		m = present(exprtypes.AnyType())
	}
	t := known(m)
	b.cache[id] = t
	return present(t)
}

func known(m member) exprtypes.Type {
	if !m.present {
		return exprtypes.AnyType()
	}
	return m.ty
}

func mappedMember(o exprtypes.ObjectType) member {
	t, ok := o.Mapped()
	return member{t, ok}
}

// combine keeps every property offered by any possible event. A missing or
// null variant keeps a known property's type.
func combine(a, b member) member {
	if !a.present {
		return b
	}
	if !b.present || a.ty == b.ty {
		return a
	}
	if x, ok := a.ty.Object(); ok {
		if y, ok := b.ty.Object(); ok {
			return present(combineObjects(x, y))
		}
	}
	if x, ok := a.ty.Array(); ok {
		if y, ok := b.ty.Array(); ok {
			return present(exprtypes.NewArrayType(combine(present(x.Elem()), present(y.Elem())).ty).Type())
		}
	}
	if a.ty.Kind() == b.ty.Kind() {
		switch a.ty.Kind() {
		case exprtypes.KindString, exprtypes.KindNumber, exprtypes.KindBool:
			return a
		case exprtypes.KindAny, exprtypes.KindNull, exprtypes.KindObject, exprtypes.KindArray:
		}
	}
	return present(exprtypes.AnyType())
}

func combineObjects(x, y exprtypes.ObjectType) exprtypes.Type {
	var o exprtypes.ObjectBuilder
	o.SetDisplayName("object")
	for name, t := range x.Properties() {
		setMember(&o, x, name, t)
	}
	if mapped := combine(mappedMember(x), mappedMember(y)); mapped.present {
		o.SetMapped(mapped.ty)
	}
	for name, t := range y.Properties() {
		current, ok := x.Property(name)
		combined := combine(member{current, ok}, present(t)).ty
		if spelling := y.PropertyName(name); spelling != name {
			o.SetProperty(spelling, combined)
		} else {
			// Set keeps the spelling x declared for name, if any.
			o.Set(name, combined)
		}
	}
	return o.Build().Type()
}

func setMember(o *exprtypes.ObjectBuilder, from exprtypes.ObjectType, key string, t exprtypes.Type) {
	if spelling := from.PropertyName(key); spelling != key {
		o.SetProperty(spelling, t)
		return
	}
	o.Set(key, t)
}
