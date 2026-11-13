package context

import (
	"maps"
	"testing"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

type modelBindings map[string]exprtypes.Type

func (bindings modelBindings) ContextType(name string) (exprtypes.Type, bool) {
	value, found := bindings[name]
	return value, found
}

func (bindings modelBindings) SetContextType(name string, value exprtypes.Type) {
	bindings[name] = value
}

func boundProperty(t *testing.T, bindings TypeBindings, root string, path ...string) exprtypes.Type {
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
	dispatch := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"dispatch_marker": exprtypes.BoolType()})
	push := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"push_marker": exprtypes.NumberType()})
	event := exprtypes.NewEventType([]exprtypes.EventVariant{{Event: "workflow_dispatch", Payload: dispatch}, {Event: "push", Payload: push}})
	bindings := modelBindings{"github": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"sha": exprtypes.StringType()}).Type()}
	SetEventPayloadType(bindings, event)
	MergeInputTypes(bindings, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"count": exprtypes.NumberType()}))
	SetDispatchInputTypes(bindings, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"flag": exprtypes.BoolType()}))
	for _, tc := range []struct {
		root string
		path []string
		kind exprtypes.Kind
	}{
		{"inputs", []string{"count"}, exprtypes.KindNumber},
		{"inputs", []string{"flag"}, exprtypes.KindBool},
		{"github", []string{"event", "inputs", "flag"}, exprtypes.KindString},
		{"github", []string{"sha"}, exprtypes.KindString},
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
			if !found || flag.Kind() != exprtypes.KindString {
				t.Fatalf("dispatch flag type = %s", flag)
			}
		} else if hasInputs {
			t.Fatal("dispatch inputs leaked into push payload")
		}
	}
	for _, original := range []exprtypes.ObjectType{event, dispatch, push} {
		if _, found := original.Property("inputs"); found {
			t.Fatal("projection modified a supplied event type")
		}
	}
}

func TestDispatchInputsDoNotInventGitHubContext(t *testing.T) {
	t.Parallel()
	bindings := modelBindings{}
	SetDispatchInputTypes(bindings, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"flag": exprtypes.BoolType()}))
	if _, found := bindings.ContextType("github"); found {
		t.Fatal("dispatch update invented an absent GitHub context")
	}
	if got := boundProperty(t, bindings, "inputs", "flag"); got.Kind() != exprtypes.KindBool {
		t.Fatalf("input flag type = %s", got)
	}
}

func TestSecretUpdatesReplaceDeclarationsAndPreserveOpenness(t *testing.T) {
	t.Parallel()
	for _, open := range []bool{false, true} {
		bindings := modelBindings{}
		SetSecretTypes(bindings, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"old": exprtypes.StringType()}))
		provided := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"Fresh": exprtypes.StringType()})
		if open {
			provided = provided.WithMapped(exprtypes.StringType())
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
			if got := boundProperty(t, bindings, "secrets", name); got.Kind() != exprtypes.KindString {
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
