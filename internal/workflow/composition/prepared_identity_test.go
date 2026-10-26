package composition

import (
	"errors"
	"testing"

	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
)

func TestPreparedBatchRejectsSwappedCallContexts(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}, {uses: $/child}]}}\n", map[string]string{
		"child/action.yml": "name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n",
	}, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	scopes, scripts := result.Batch.Snapshot()
	if len(scopes) != 3 || len(scripts) != 2 {
		t.Fatal("fixture did not expand both calls")
	}
	scopes[1].Context, scopes[2].Context = scopes[2].Context, scopes[1].Context
	if _, err := scriptexecution.NewBatch(result.Batch.Invocations(), scopes, scripts, true); !errors.Is(err, scriptexecution.ErrInvalidBatch) {
		t.Fatalf("swapped call contexts accepted: %v", err)
	}
}
