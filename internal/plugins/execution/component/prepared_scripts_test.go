package component

import (
	"context"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	preparedbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/preparedscripts"
)

func TestPreparedScriptsRejectForeignReference(t *testing.T) {
	called := false
	scope := preparationScope{prepare: func(context.Context, checkinputs.OccurrenceID, *string) (*checkinputs.PreparedScript, error) {
		called = true
		return nil, nil
	}, occurrences: map[checkinputs.OccurrenceID]struct{}{"known": {}}}
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
	selected := checkinputs.Script{Occurrence: checkinputs.ScriptOccurrence{ID: "selected", Kind: checkinputs.RunScript}}
	action := checkinputs.ActionInput{Occurrence: checkinputs.ScriptOccurrence{ID: "action-input", Kind: checkinputs.ActionInputScript}}
	calls := 0
	input := checkinputs.Input{Scripts: []checkinputs.Script{selected}, ActionInputs: []checkinputs.ActionInput{action}}
	scope := preparationScope{occurrences: suppliedOccurrences(input), prepare: func(_ context.Context, id checkinputs.OccurrenceID, _ *string) (*checkinputs.PreparedScript, error) {
		calls++
		if id != selected.Occurrence.ID {
			t.Fatalf("wrong occurrence reached preparation: %s", id)
		}
		return &checkinputs.PreparedScript{Text: "selected"}, nil
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
