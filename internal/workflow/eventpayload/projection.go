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
func FromTriggers(triggers []Trigger) *expr.ObjectType {
	b := builder{cache: map[int]expr.Type{}, active: map[int]bool{}}
	var selected []expr.EventVariant
	for _, trigger := range triggers {
		name := trigger.Name
		if name == "pull_request_target" {
			name = "pull_request"
		}
		variants, known := payload.Variants(name)
		var dispatch *expr.ObjectType
		if trigger.Name == "repository_dispatch" {
			// OpenAPI indexes this shared schema by an example event_type. That
			// example is not a restriction on user-defined dispatch activities.
			var combined expr.Type
			for _, action := range slices.Sorted(maps.Keys(variants)) {
				combined = combine(combined, b.build(variants[action]))
			}
			if object, ok := combined.(*expr.ObjectType); ok {
				copyObject := *object
				copyObject.Props = maps.Clone(object.Props)
				copyObject.Props["client_payload"] = expr.NewEmptyObjectType()
				dispatch = &copyObject
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
			if dispatch != nil {
				object = dispatch
			} else if known && ok {
				if projected, ok := b.build(id).(*expr.ObjectType); ok {
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
// so narrowing cannot discard it. Clone only the path being changed.
func workflowRunPayload(object *expr.ObjectType) *expr.ObjectType {
	run, ok := object.Props["workflow_run"].(*expr.ObjectType)
	if !ok {
		return object
	}
	repository, ok := run.Props["head_repository"].(*expr.ObjectType)
	if !ok {
		return object
	}
	copyRepository := *repository
	copyRepository.Props = maps.Clone(repository.Props)
	copyRepository.Props["homepage"] = expr.StringType{}
	copyRun := *run
	copyRun.Props = maps.Clone(run.Props)
	copyRun.Props["head_repository"] = &copyRepository
	copyObject := *object
	copyObject.Props = maps.Clone(object.Props)
	copyObject.Props["workflow_run"] = &copyRun
	return &copyObject
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
	case "union", "intersection":
		for _, alternative := range n.Alternatives {
			t = combine(t, b.build(alternative))
		}
	case "object":
		o := expr.NewEmptyStrictObjectType()
		o.DisplayName = "object"
		for _, name := range slices.Sorted(maps.Keys(n.Properties)) {
			o.SetProperty(name, known(b.build(n.Properties[name].Type)))
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
			o.PropertyNames = maps.Clone(x.PropertyNames)
			o.DisplayName = "object"
			o.Mapped = combine(x.Mapped, y.Mapped)
			for name, t := range y.Props {
				o.Props[name] = combine(o.Props[name], t)
				if original, ok := y.PropertyNames[name]; ok {
					if o.PropertyNames == nil {
						o.PropertyNames = map[string]string{}
					}
					o.PropertyNames[name] = original
				}
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
