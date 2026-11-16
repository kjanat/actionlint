package component

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	shellinputbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/shellscriptinputs"
)

func TestPreparedScriptsRejectForeignReference(t *testing.T) {
	t.Parallel()
	called := false
	scope := shellScriptInputScope{prepare: func(context.Context, invocation.ID, *string) (*inputs.ShellScriptInput, error) {
		called = true
		return nil, nil
	}, occurrences: map[invocation.ID]struct{}{"known": {}}}
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

func TestStandaloneAnalyzerReceivesShellScriptInputs(t *testing.T) {
	t.Parallel()
	for _, invoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "declared", true: "invoked"}[invoked], func(t *testing.T) {
			t.Parallel()
			script := inputs.Script{
				Text: "echo ${{ inputs.name }}", Shell: "bash",
				Occurrence: invocation.Script{ID: "run", Kind: invocation.ScriptRun},
			}
			input := inputs.AnalyzerInput{}
			if invoked {
				input.Invoked = []inputs.Script{script}
			} else {
				input.Scripts = []inputs.Script{script}
			}
			ctx := context.WithValue(t.Context(), shellScriptInputsKey{}, newShellScriptInputScope(input))
			result, err := (shellScriptInputs{}).ForScript(ctx, "run", shellinputbind.NoneString())
			accepted, ok := result.(shellinputbind.ResultOptionInputStringOk)
			if err != nil || !ok || !accepted.Value.IsSome {
				t.Fatalf("standalone script unavailable: %v %v", result, err)
			}
			prepared := accepted.Value.Value
			if strings.Contains(prepared.Text, "${{") || !strings.Contains(prepared.Text, "echo ") || !prepared.Dialect.IsSome || prepared.Dialect.Value != "bash" {
				t.Fatalf("wrong masked Bash input: %+v", prepared)
			}
			result, err = (shellScriptInputs{}).ForScript(ctx, "foreign", shellinputbind.NoneString())
			if _, ok := result.(shellinputbind.ResultOptionInputStringErr); err != nil || !ok {
				t.Fatalf("foreign occurrence accepted: %v %v", result, err)
			}
		})
	}
}

func TestStandaloneAnalyzerUsesExplicitShellScriptInputBuilder(t *testing.T) {
	t.Parallel()
	script := inputs.Script{Occurrence: invocation.Script{ID: "run", Kind: invocation.ScriptRun}}
	input := inputs.AnalyzerInput{Scripts: []inputs.Script{script}, BuildShellScriptInput: func(context.Context, invocation.ID, *string) (*inputs.ShellScriptInput, error) {
		return &inputs.ShellScriptInput{Text: "custom input"}, nil
	}}
	ctx := context.WithValue(t.Context(), shellScriptInputsKey{}, newShellScriptInputScope(input))
	result, err := (shellScriptInputs{}).ForScript(ctx, "run", shellinputbind.NoneString())
	accepted, ok := result.(shellinputbind.ResultOptionInputStringOk)
	if err != nil || !ok || !accepted.Value.IsSome || accepted.Value.Value.Text != "custom input" {
		t.Fatalf("custom builder was replaced: %v %v", result, err)
	}
}

func TestPreparationScopeExcludesUnselectedAndNonRunOccurrences(t *testing.T) {
	t.Parallel()
	selected := inputs.Script{Occurrence: invocation.Script{ID: "selected", Kind: invocation.ScriptRun}}
	action := inputs.ActionInput{Occurrence: invocation.Script{ID: "action-input", Kind: invocation.ScriptActionInput}}
	calls := 0
	input := inputs.AnalyzerInput{Scripts: []inputs.Script{selected}, ActionInputs: []inputs.ActionInput{action}}
	scope := shellScriptInputScope{occurrences: suppliedOccurrences(input), prepare: func(_ context.Context, id invocation.ID, _ *string) (*inputs.ShellScriptInput, error) {
		calls++
		if id != selected.Occurrence.ID {
			t.Fatalf("wrong occurrence reached preparation: %s", id)
		}
		return &inputs.ShellScriptInput{Text: "selected"}, nil
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
