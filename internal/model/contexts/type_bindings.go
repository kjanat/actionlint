package context

import exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

// TypeBindings reads and replaces expression context types. Implementations
// store immutable type values; these operations do not evaluate expressions.
type TypeBindings interface {
	ContextType(string) (exprtypes.Type, bool)
	SetContextType(string, exprtypes.Type)
}

// SetSecretTypes replaces secret declarations and adds GitHub's supplied names.
// The supplied type determines whether undeclared names are allowed.
func SetSecretTypes(bindings TypeBindings, secrets exprtypes.ObjectType) {
	var builder exprtypes.ObjectBuilder
	for name, value := range builtinSecretProps {
		builder.Set(name, value)
	}
	for name, value := range secrets.Properties() {
		builder.SetProperty(secrets.PropertyName(name), value)
	}
	if mapped, ok := secrets.Mapped(); ok {
		builder.SetMapped(mapped)
	}
	bindings.SetContextType("secrets", builder.Build().Type())
}

// MergeInputTypes merges supplied definitions into the inputs context. Workflows
// with multiple input-bearing triggers use this to combine their definitions.
func MergeInputTypes(bindings TypeBindings, inputs exprtypes.ObjectType) {
	current, _ := bindings.ContextType("inputs")
	object, ok := current.Object()
	if !ok || object.Len() == 0 && object.IsStrict() {
		bindings.SetContextType("inputs", inputs.Type())
		return
	}
	bindings.SetContextType("inputs", object.Type().Merge(inputs.Type()))
}

// SetEventPayloadType installs the event payload projection beneath github.event.
func SetEventPayloadType(bindings TypeBindings, event exprtypes.ObjectType) {
	current, _ := bindings.ContextType("github")
	if github, ok := current.Object(); ok {
		bindings.SetContextType("github", github.With("event", event.Type()).Type())
	}
}

// SetDispatchInputTypes merges declared input types into inputs and adds their
// string-valued types at github.event.inputs, including its workflow_dispatch
// variant.
func SetDispatchInputTypes(bindings TypeBindings, inputs exprtypes.ObjectType) {
	MergeInputTypes(bindings, inputs)
	var builder exprtypes.ObjectBuilder
	for name := range inputs.Properties() {
		builder.SetProperty(inputs.PropertyName(name), exprtypes.StringType())
	}
	projected := builder.Build().Type()
	current, _ := bindings.ContextType("github")
	github, ok := current.Object()
	if !ok {
		return
	}
	eventType, ok := github.Property("event")
	if !ok {
		return
	}
	event, ok := eventType.Object()
	if !ok {
		return
	}
	variants := event.EventVariants()
	for index, variant := range variants {
		if variant.Event == "workflow_dispatch" {
			variants[index].Payload = variant.Payload.With("inputs", projected)
		}
	}
	updated := event.With("inputs", projected).WithEventVariants(variants)
	bindings.SetContextType("github", github.With("event", updated.Type()).Type())
}
