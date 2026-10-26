package component

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	preparedbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/preparedscripts"
)

func TestPreparedScriptsRejectForeignReference(t *testing.T) {
	called := false
	scope := preparationScope{prepare: func(context.Context, analyzer.OccurrenceID, *string) (*analyzer.PreparedScript, error) {
		called = true
		return nil, nil
	}, occurrences: map[analyzer.OccurrenceID]struct{}{"known": {}}}
	ctx := context.WithValue(t.Context(), preparedScriptsKey{}, scope)
	result, err := (preparedScripts{}).Prepare(ctx, "foreign", preparedbind.NoneString())
	if _, ok := result.(preparedbind.ResultOptionPreparedStringErr); !ok || err != nil || called {
		t.Fatalf("foreign reference reached callback: %v, %v", result, err)
	}
	result, err = (preparedScripts{}).Prepare(ctx, "known", preparedbind.NoneString())
	accepted, ok := result.(preparedbind.ResultOptionPreparedStringOk)
	if !ok || accepted.Value.IsSome || err != nil || !called {
		t.Fatalf("unsupported script result: %v, %v", result, err)
	}
}

func TestPreparationScopeExcludesUnselectedAndNonRunOccurrences(t *testing.T) {
	selected := analyzer.Script{Occurrence: analyzer.ScriptOccurrence{ID: "selected", Kind: analyzer.RunScript}}
	action := analyzer.ActionInput{Occurrence: analyzer.ScriptOccurrence{ID: "action-input", Kind: analyzer.ActionInputScript}}
	calls := 0
	input := analyzer.Input{Scripts: []analyzer.Script{selected}, ActionInputs: []analyzer.ActionInput{action}}
	scope := preparationScope{occurrences: suppliedOccurrences(input), prepare: func(_ context.Context, id analyzer.OccurrenceID, _ *string) (*analyzer.PreparedScript, error) {
		calls++
		if id != selected.Occurrence.ID {
			t.Fatalf("wrong occurrence reached preparation: %s", id)
		}
		return &analyzer.PreparedScript{Text: "selected"}, nil
	}}
	ctx := context.WithValue(t.Context(), preparedScriptsKey{}, scope)
	for _, id := range []string{"", "unselected", "action-input", "entry", "call", "unknown"} {
		result, err := (preparedScripts{}).Prepare(ctx, id, preparedbind.NoneString())
		if _, ok := result.(preparedbind.ResultOptionPreparedStringErr); !ok || err != nil {
			t.Fatalf("unauthorized %q accepted: %v %v", id, result, err)
		}
	}
	if calls != 0 {
		t.Fatalf("unauthorized calls reached callback: %d", calls)
	}
	result, err := (preparedScripts{}).Prepare(ctx, "selected", preparedbind.NoneString())
	accepted, ok := result.(preparedbind.ResultOptionPreparedStringOk)
	if !ok || !accepted.Value.IsSome || accepted.Value.Value.Text != "selected" || err != nil || calls != 1 {
		t.Fatalf("selected preparation: %v %v calls=%d", result, err, calls)
	}
}
