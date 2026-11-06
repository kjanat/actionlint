package runner

import (
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestContainerTargetsUseProviderEvidence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		os        runnerapi.OS
		freshness runnerapi.Freshness
		complete  bool
		want      int
	}{
		{"Windows", runnerapi.Windows, runnerapi.Fresh, true, 1},
		{"macOS", runnerapi.MacOS, runnerapi.Fresh, true, 1},
		{"Linux overrides label", runnerapi.Linux, runnerapi.Fresh, true, 0},
		{"unknown", runnerapi.UnknownOS, runnerapi.Fresh, true, 0},
		{"stale", runnerapi.Windows, runnerapi.Stale, true, 0},
		{"incomplete", runnerapi.Windows, runnerapi.Fresh, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := runnerWorkflow(t, "ubuntu-slim")
			provider := &testProvider{resolve: func(request runnerapi.Request) ([]runnerapi.Answer, error) {
				answers := make([]runnerapi.Answer, len(request.Selectors))
				for i := range answers {
					answers[i] = runnerapi.Answer{Resolution: runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "configured", OS: tc.os}}}, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "custom", Scope: request.Scope, Freshness: tc.freshness, Complete: tc.complete}}
				}
				return answers, nil
			}}
			knowledge, err := Prepare(t.Context(), fixture.Snapshot, nil, runnerapi.Options{Provider: "queues", Providers: []runnerapi.Registration{{Provider: provider, ImplementationID: "test"}}}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			if got := knowledge.ContainerLimitations(fixture.Views["test"]); len(got) != tc.want {
				t.Fatalf("targets=%+v, want %d", got, tc.want)
			}
		})
	}
}

func TestContainerTargetExcludesOnlyProvedFalseConditions(t *testing.T) {
	target := ContainerTarget{OS: runnerapi.Windows}
	for _, tc := range []struct {
		name   string
		guards []string
		want   bool
	}{
		{"no guard", nil, true},
		{"wrong OS", []string{"runner.os == 'Linux'"}, false},
		{"matching OS", []string{"runner.os == 'Windows'"}, true},
		{"unknown context", []string{"inputs.run"}, true},
		{"unknown and false", []string{"inputs.run && false"}, false},
		{"invalid expression", []string{"contains("}, true},
		{"enclosing skipped call", []string{"always()", "false"}, false},
		{"status and OS", []string{"success() && runner.os == 'Linux'"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := target.MayRun(tc.guards...); got != tc.want {
				t.Fatalf("MayRun(%v)=%t, want %t", tc.guards, got, tc.want)
			}
		})
	}
}
