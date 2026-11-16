package contexts

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestEventTypeNarrowingPreservesUnknownVariants(t *testing.T) {
	t.Parallel()
	push := types.EventVariant{Event: "push", Payload: types.NewEmptyStrictObjectType()}
	issue := types.EventVariant{Event: "issues", Action: "opened", Payload: types.NewStrictObjectType(map[string]types.Type{"action": types.StringType()})}
	unknown := types.EventVariant{Payload: types.NewEmptyObjectType()}
	event := types.NewEventType([]types.EventVariant{push, issue, unknown})
	bindings := modelBindings{"github": types.NewStrictObjectType(map[string]types.Type{"event": event.Type()}).Type()}
	for _, input := range []string{"github.event_name == 'push'", "github.event.action == '0'", "format('pu{0}', 'sh') == github.event_name"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			node := syntax.ParseAssignedExpression("${{ " + input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			overrides, possible := NarrowEventTypes(node, true, bindings.ContextType)
			if !possible || len(overrides) != 1 {
				t.Fatalf("narrowing=%v possible=%t, want a possible branch and one refinement", overrides, possible)
			}
			refined := modelBindings(overrides)
			payload, _ := boundProperty(t, refined, "github", "event").Object()
			var selected []string
			for _, variant := range payload.EventVariants() {
				selected = append(selected, variant.Event)
			}
			if !reflect.DeepEqual(selected, []string{"push", ""}) {
				t.Fatalf("selected=%v, want push and the unknown event", selected)
			}
			if len(event.EventVariants()) != 3 {
				t.Fatal("narrowing modified the supplied event")
			}
		})
	}
}

func TestEventTypeNarrowingDistinguishesUnknownAndUnreachable(t *testing.T) {
	t.Parallel()
	event := types.NewEventType([]types.EventVariant{{Event: "push", Payload: types.NewEmptyStrictObjectType()}})
	bindings := modelBindings{"github": types.NewStrictObjectType(map[string]types.Type{"event": event.Type()}).Type()}
	for _, tc := range []struct {
		input    string
		bindings modelBindings
		possible bool
	}{
		{"github.event_name == 'issues'", bindings, false},
		{"env.DYNAMIC", bindings, true},
		{"github.event_name == 'issues'", modelBindings{}, true},
	} {
		node := syntax.ParseAssignedExpression("${{ " + tc.input + " }}")
		if node == nil {
			t.Fatal("expression did not parse")
		}
		overrides, possible := NarrowEventTypes(node, true, tc.bindings.ContextType)
		if possible != tc.possible || len(overrides) != 0 {
			t.Fatalf("%s: overrides=%v possible=%t, want %t", tc.input, overrides, possible, tc.possible)
		}
	}
}
