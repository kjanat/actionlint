package checking_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
)

func TestScriptBudgetWithoutRegisteredTools(t *testing.T) {
	t.Parallel()
	content := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" +
		strings.Repeat("      - run: echo ok\n", invocation.DefaultMaxScripts) +
		"      - uses: actions/github-script@v7\n        with: {script: console.log(1)}\n"
	loads := 0
	result, err := checking.Analyze(t.Context(), checking.Options{
		WorkingDir: t.TempDir(),
		LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) {
			loads++
			return nil, errors.New("unexpected script engine load")
		},
	}, []workspace.Source{{Path: "budget.yml", Content: []byte(content)}})
	if !errors.Is(err, invocation.ErrScriptBudget) || result == nil || loads != 0 {
		t.Fatalf("no-tool analysis lost budget or loaded an engine: result=%v error=%v loads=%d", result != nil, err, loads)
	}
}
