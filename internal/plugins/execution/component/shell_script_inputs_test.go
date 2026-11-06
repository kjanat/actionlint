package component

import (
	"context"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	shellinputbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/shellscriptinputs"
)

func TestPreparedScriptsRejectForeignReference(t *testing.T) {
	called := false
	scope := shellScriptInputScope{prepare: func(context.Context, checkinputs.OccurrenceID, *string) (*checkinputs.ShellScriptInput, error) {
		called = true
		return nil, nil
	}, occurrences: map[checkinputs.OccurrenceID]struct{}{"known": {}}}
	ctx := context.WithValue(t.Context(), shellScriptInputsKey{}, scope)
	result, err := (shellScriptInputs{}).ForScript(ctx, "foreign", shellinputbind.NoneString())
	if _, ok := result.(shellinputbind.ResultOptionInputStringErr); !ok || err != nil || called {
		t.Fatalf("foreign reference reached callback: %v, %v", result, err)
	}
	result, err = (shellScriptInputs{}).ForScript(ctx, "known", shellinputbind.NoneString())
	accepted, ok := result.(shellinputbind.ResultOptionInputStringOk)
	if !ok || accepted.Value.IsSome || err != nil || !called {
		t.Fatalf("unsupported script result: %v, %v", result, err)
	}
}

func TestPreparationScopeExcludesUnselectedAndNonRunOccurrences(t *testing.T) {
	selected := checkinputs.Script{Occurrence: checkinputs.ScriptOccurrence{ID: "selected", Kind: checkinputs.RunScript}}
	action := checkinputs.ActionInput{Occurrence: checkinputs.ScriptOccurrence{ID: "action-input", Kind: checkinputs.ActionInputScript}}
	calls := 0
	input := checkinputs.Input{Scripts: []checkinputs.Script{selected}, ActionInputs: []checkinputs.ActionInput{action}}
	scope := shellScriptInputScope{occurrences: suppliedOccurrences(input), prepare: func(_ context.Context, id checkinputs.OccurrenceID, _ *string) (*checkinputs.ShellScriptInput, error) {
		calls++
		if id != selected.Occurrence.ID {
			t.Fatalf("wrong occurrence reached preparation: %s", id)
		}
		return &checkinputs.ShellScriptInput{Text: "selected"}, nil
	}}
	ctx := context.WithValue(t.Context(), shellScriptInputsKey{}, scope)
	for _, id := range []string{"", "unselected", "action-input", "entry", "call", "unknown"} {
		result, err := (shellScriptInputs{}).ForScript(ctx, id, shellinputbind.NoneString())
		if _, ok := result.(shellinputbind.ResultOptionInputStringErr); !ok || err != nil {
			t.Fatalf("unauthorized %q accepted: %v %v", id, result, err)
		}
	}
	if calls != 0 {
		t.Fatalf("unauthorized calls reached callback: %d", calls)
	}
	result, err := (shellScriptInputs{}).ForScript(ctx, "selected", shellinputbind.NoneString())
	accepted, ok := result.(shellinputbind.ResultOptionInputStringOk)
	if !ok || !accepted.Value.IsSome || accepted.Value.Value.Text != "selected" || err != nil || calls != 1 {
		t.Fatalf("selected preparation: %v %v calls=%d", result, err, calls)
	}
}
