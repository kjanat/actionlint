package eventpayload

import (
	"maps"
	"slices"

	expr "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/webhook"
	"actionlint.kjanat.dev/v2/internal/webhook/payload"
)

// Trigger identifies a possible event and optional activity filters. Unknown
// events/activities stay open, including reusable workflows' inherited events.
type Trigger struct {
	Name    string
	Actions []string
}

// FromTriggers builds a payload type from the selected events and activities.
func FromTriggers(triggers []Trigger) expr.ObjectType {
	b := builder{cache: map[int]expr.Type{}, active: map[int]bool{}}
	var selected []expr.EventVariant
	for _, trigger := range triggers {
		name := trigger.Name
		if name == "pull_request_target" {
			name = "pull_request"
		}
		variants, known := payload.Variants(name)
		var dispatch expr.ObjectType
		hasDispatch := false
		if trigger.Name == "repository_dispatch" {
			// OpenAPI indexes this shared schema by an example event_type. That
			// example is not a restriction on user-defined dispatch activities.
			var combined member
			for _, action := range slices.Sorted(maps.Keys(variants)) {
				combined = combine(combined, b.build(variants[action]))
			}
			if object, ok := combined.ty.Object(); ok {
				dispatch = object.With("client_payload", expr.NewEmptyObjectType().Type())
				hasDispatch = true
			}
		}
		actions := trigger.Actions
		if len(actions) == 0 {
			actions, _ = webhook.DefaultActivities(trigger.Name)
		}
		if len(actions) == 0 {
			// Events without a types filter can still carry webhook actions, e.g.
			// deployment's "created" payload. The registry decides syntax validity;
			// the catalogue supplies each possible payload for an actionless trigger.
			if trigger.Name != "repository_dispatch" {
				actions = slices.Sorted(maps.Keys(variants))
			}
			if len(actions) == 0 {
				actions = []string{""}
			}
		}
		for _, action := range actions {
			id, ok := variants[action]
			object := expr.NewEmptyObjectType()
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
			selected = append(selected, expr.EventVariant{Event: event, Action: action, Payload: object})
		}
	}
	return expr.NewEventType(selected)
}

// Keep the established Actions repository metadata overlay on each alternative,
// so narrowing cannot discard it. Copy only the path being changed.
func workflowRunPayload(object expr.ObjectType) expr.ObjectType {
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
	repository = repository.With("homepage", expr.StringType())
	run = run.With("head_repository", repository.Type())
	return object.With("workflow_run", run.Type())
}

// member is a payload type that may be absent, as for "absent" and "null"
// schema nodes and for a strict object's undeclared properties. When present
// is false, ty is the zero Type.
type member struct {
	ty      expr.Type
	present bool
}

func present(t expr.Type) member { return member{t, true} }

type builder struct {
	cache  map[int]expr.Type
	active map[int]bool
}

func (b *builder) build(id int) member {
	if t, ok := b.cache[id]; ok {
		return present(t)
	}
	// Recursive payloads are finite at runtime but have unbounded schema depth.
	// Keep the recursive edge open instead of expanding or rejecting it.
	if b.active[id] {
		return present(expr.AnyType())
	}
	b.active[id] = true
	defer delete(b.active, id)
	n := payload.Node(id)
	var m member
	switch n.Kind {
	case "absent", "null":
		return member{}
	case "string":
		m = present(expr.StringType())
	case "number":
		m = present(expr.NumberType())
	case "boolean":
		m = present(expr.BoolType())
	case "array":
		m = present(expr.NewArrayType(known(b.build(n.Element))).Type())
	case "union", "intersection":
		for _, alternative := range n.Alternatives {
			m = combine(m, b.build(alternative))
		}
	case "object":
		var o expr.ObjectBuilder
		o.SetDisplayName("object")
		for _, name := range slices.Sorted(maps.Keys(n.Properties)) {
			o.SetProperty(name, known(b.build(n.Properties[name].Type)))
		}
		if n.Mapped != nil {
			o.SetMapped(known(b.build(*n.Mapped)))
		}
		m = present(o.Build().Type())
	default:
		m = present(expr.AnyType())
	}
	t := known(m)
	b.cache[id] = t
	return present(t)
}

// known types an absent member as any.
func known(m member) expr.Type {
	if !m.present {
		return expr.AnyType()
	}
	return m.ty
}

func mappedMember(o expr.ObjectType) member {
	t, ok := o.Mapped()
	return member{t, ok}
}

// combine preserves all properties offered by any possible event. Missing and
// null variants are not a reason to erase a known property's type.
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
			return present(expr.NewArrayType(combine(present(x.Elem()), present(y.Elem())).ty).Type())
		}
	}
	if a.ty.Kind() == b.ty.Kind() {
		switch a.ty.Kind() {
		case expr.KindString, expr.KindNumber, expr.KindBool:
			return a
		case expr.KindAny, expr.KindNull, expr.KindObject, expr.KindArray:
		}
	}
	return present(expr.AnyType())
}

// combineObjects returns a strict object holding the properties of x and y.
// A property in both has the combined type. The declared spelling from y wins
// over the one from x.
func combineObjects(x, y expr.ObjectType) expr.Type {
	var o expr.ObjectBuilder
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

// setMember stores t under key in o and keeps the spelling that from declared
// for key. A declared spelling always lower-cases to its key, so SetProperty
// stores it under the same key.
func setMember(o *expr.ObjectBuilder, from expr.ObjectType, key string, t expr.Type) {
	if spelling := from.PropertyName(key); spelling != key {
		o.SetProperty(spelling, t)
		return
	}
	o.Set(key, t)
}
