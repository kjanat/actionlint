package contexts

import (
	"maps"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

type modelBindings map[string]types.Type

func (bindings modelBindings) ContextType(name string) (types.Type, bool) {
	value, found := bindings[name]
	return value, found
}

func (bindings modelBindings) SetContextType(name string, value types.Type) {
	bindings[name] = value
}

func boundProperty(t *testing.T, bindings TypeBindings, root string, path ...string) types.Type {
	t.Helper()
	value, found := bindings.ContextType(root)
	if !found {
		t.Fatalf("missing context %q", root)
	}
	for _, name := range path {
		object, ok := value.Object()
		if !ok {
			t.Fatalf("%s.%s: expected object, got %s", root, name, value)
		}
		value, found = object.Property(name)
		if !found {
			t.Fatalf("%s: missing property %q", root, name)
		}
	}
	return value
}

func TestDispatchInputsUpdateOnlyTheirEventVariant(t *testing.T) {
	t.Parallel()
	dispatch := types.NewStrictObjectType(map[string]types.Type{"dispatch_marker": types.BoolType()})
	push := types.NewStrictObjectType(map[string]types.Type{"push_marker": types.NumberType()})
	event := types.NewEventType([]types.EventVariant{{Event: "workflow_dispatch", Payload: dispatch}, {Event: "push", Payload: push}})
	bindings := modelBindings{"github": types.NewStrictObjectType(map[string]types.Type{"sha": types.StringType()}).Type()}
	SetEventPayloadType(bindings, event)
	MergeInputTypes(bindings, types.NewStrictObjectType(map[string]types.Type{"count": types.NumberType()}))
	SetDispatchInputTypes(bindings, types.NewStrictObjectType(map[string]types.Type{"flag": types.BoolType()}))
	for _, tc := range []struct {
		root string
		path []string
		kind types.TypeKind
	}{
		{"inputs", []string{"count"}, types.KindNumber},
		{"inputs", []string{"flag"}, types.KindBool},
		{"github", []string{"event", "inputs", "flag"}, types.KindString},
		{"github", []string{"sha"}, types.KindString},
	} {
		if got := boundProperty(t, bindings, tc.root, tc.path...); got.Kind() != tc.kind {
			t.Fatalf("%s.%v: got %s, want %v", tc.root, tc.path, got, tc.kind)
		}
	}
	updated, _ := boundProperty(t, bindings, "github", "event").Object()
	variants := updated.EventVariants()
	if len(variants) != 2 {
		t.Fatalf("lost event variants: %+v", variants)
	}
	for _, variant := range variants {
		inputs, hasInputs := variant.Payload.Property("inputs")
		if variant.Event == "workflow_dispatch" {
			object, ok := inputs.Object()
			if !hasInputs || !ok {
				t.Fatal("dispatch variant has no input projection")
			}
			flag, found := object.Property("flag")
			if !found || flag.Kind() != types.KindString {
				t.Fatalf("dispatch flag type = %s", flag)
			}
		} else if hasInputs {
			t.Fatal("dispatch inputs leaked into push payload")
		}
	}
	for _, original := range []types.ObjectType{event, dispatch, push} {
		if _, found := original.Property("inputs"); found {
			t.Fatal("projection modified a supplied event type")
		}
	}
}

func TestDispatchInputsDoNotInventGitHubContext(t *testing.T) {
	t.Parallel()
	bindings := modelBindings{}
	SetDispatchInputTypes(bindings, types.NewStrictObjectType(map[string]types.Type{"flag": types.BoolType()}))
	if _, found := bindings.ContextType("github"); found {
		t.Fatal("dispatch update invented an absent GitHub context")
	}
	if got := boundProperty(t, bindings, "inputs", "flag"); got.Kind() != types.KindBool {
		t.Fatalf("input flag type = %s", got)
	}
}

func TestSecretUpdatesReplaceDeclarationsAndPreserveOpenness(t *testing.T) {
	t.Parallel()
	for _, open := range []bool{false, true} {
		bindings := modelBindings{}
		SetSecretTypes(bindings, types.NewStrictObjectType(map[string]types.Type{"old": types.StringType()}))
		provided := types.NewStrictObjectType(map[string]types.Type{"Fresh": types.StringType()})
		if open {
			provided = provided.WithMapped(types.StringType())
		}
		SetSecretTypes(bindings, provided)
		secrets, _ := boundProperty(t, bindings, "secrets").Object()
		if secrets.IsStrict() == open {
			t.Fatalf("strict=%t, want open=%t", secrets.IsStrict(), open)
		}
		if _, found := maps.Collect(secrets.Properties())["old"]; found {
			t.Fatal("previous secret declaration survived replacement")
		}
		for _, name := range []string{"github_token", "actions_step_debug", "actions_runner_debug", "fresh"} {
			if got := boundProperty(t, bindings, "secrets", name); got.Kind() != types.KindString {
				t.Fatalf("secret %s type = %s", name, got)
			}
		}
		if secrets.PropertyName("fresh") != "Fresh" {
			t.Fatal("secret declaration spelling changed")
		}
		if _, found := maps.Collect(provided.Properties())["github_token"]; found {
			t.Fatal("secret update modified the supplied type")
		}
	}
}
