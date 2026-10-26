package runner

import (
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func TestPreparedMalformedSelectorsRemainUnknown(t *testing.T) {
	for _, selection := range []string{
		`${{ null }}`,
		`${{ fromJSON('{"labels":["windows-latest",{}]}') }}`,
		`${{ fromJSON('{"labels":[["windows-latest"]]}') }}`,
		`${{ fromJSON('{"labels":"windows-latest","group":["prod"]}') }}`,
		`{labels: '${{ null }}'}`,
	} {
		t.Run(selection, func(t *testing.T) {
			w := runnerWorkflow(t, selection)
			k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			got := k.Job(w.Views["test"])
			if got.Platform != Any || got.Shell != "" || len(got.Findings) != 0 {
				t.Fatalf("invalid shape acquired meaning: %#v", got)
			}
		})
	}
}

func TestPartialSelectorsPreserveConstraints(t *testing.T) {
	for _, selection := range []string{
		`{group: prod, labels: ['${{ matrix.os }}', "${{ 'x64' }}"]}`,
		`${{ matrix.runner }}`,
	} {
		w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix:\n        os: [windows-latest]\n        runner: [{group: prod, labels: [windows-latest, x64]}]\n        unknown: '${{ fromJSON(vars.AXIS) }}'\n    runs-on: "+selection+"\n    steps: [{run: echo ok}]\n")
		selectors := Selectors(w.Views["test"])
		if len(selectors) != 2 {
			t.Fatalf("%#v", selectors)
		}
		s := selectors[1]
		if !s.Incomplete || s.Group == nil || s.Group.Value != runnerapi.Literal("prod") || len(s.Labels) != 2 || s.Labels[0].Value != runnerapi.Literal("windows-latest") || s.Labels[1].Value != runnerapi.Literal("x64") {
			t.Fatalf("lost constraints: %#v", s)
		}
		k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
		if err != nil {
			t.Fatal(err)
		}
		if got := k.Job(w.Views["test"]); got.Shell != "" || len(got.Findings) != 0 {
			t.Fatalf("partial alternatives claimed certainty: %#v", got)
		}
	}
}

func TestDynamicMatrixSelector(t *testing.T) {
	w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy: {matrix: '${{ fromJSON(vars.MATRIX) }}'}\n    runs-on: ${{ matrix.os }}\n    steps: [{run: echo ok}]\n")
	k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Job(w.Views["test"]); len(got.Findings) != 0 || got.Shell != "" {
		t.Fatalf("dynamic matrix inferred certainty: %#v", got)
	}
}

func TestPreparedNativeRunnerFindings(t *testing.T) {
	for _, tc := range []struct {
		selection string
		count     int
	}{
		{"ubuntu-latest", 0},
		{"[windows-latest, linux]", 1},
		{"unknown-runner", 1},
		{`${{ fromJSON('{"group":"prod","labels":["linux"]}') }}`, 0},
	} {
		w := runnerWorkflow(t, tc.selection)
		k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
		if err != nil {
			t.Fatal(err)
		}
		got := k.Job(w.Views["test"]).Findings
		if len(got) != tc.count {
			t.Fatalf("%s: %#v", tc.selection, got)
		}
	}
}
