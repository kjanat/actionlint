package runners

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
)

func TestRunnerSupportUsesReachableMatrixRows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, matrix, selector string
		want                   int
	}{
		{"static selector with all rows excluded", "{x: [a], exclude: [{x: a}]}", "[self-hosted, linux, ARM]", 0},
		{"static selector with reachable row", "{x: [a, b], exclude: [{x: a}]}", "[self-hosted, linux, ARM]", 1},
		{"static selector with multiple rows", "{x: [a, b]}", "[self-hosted, linux, ARM]", 1},
		{"static selector with dynamic matrix", "{x: '${{ fromJSON(vars.X) }}'}", "[self-hosted, linux, ARM]", 1},
		{"matrix excludes ARM32 selector", "{runner: [[self-hosted, linux, ARM], [self-hosted, linux, ARM64]], exclude: [{runner: [self-hosted, linux, ARM]}]}", "'${{ matrix.runner }}'", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workflow := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: "+tc.matrix+"\n    runs-on: "+tc.selector+"\n    steps: [{run: echo ok}]\n")
			knowledge, err := Prepare(t.Context(), workflow.Snapshot, nil, runnerapi.Options{Service: service(t, "github.com", ""), AsOf: date(t, "2026-11-14")}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			if got := knowledge.Support(workflow.Views["test"], runnerapi.JobExecution); len(got) != tc.want {
				t.Fatalf("support=%+v, want %d", got, tc.want)
			}
		})
	}
}
