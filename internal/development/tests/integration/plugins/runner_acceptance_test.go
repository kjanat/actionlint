package plugins_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
)

func TestRunnerProviderConjunctionAndRefresh(t *testing.T) {
	t.Parallel()
	p := loadRunnerFixture(t)
	request := runnerFixtureRequest(t)
	request.Selectors = []runner.Selector{
		{Labels: []runner.Constraint{{Value: runner.Literal("buildkite-windows")}, {Value: runner.Literal("buildkite-linux")}}},
		{Labels: []runner.Constraint{{Value: runner.Literal("buildkite-windows")}}, Group: &runner.Constraint{Value: runner.Literal("wrong")}},
		{Labels: []runner.Constraint{{Value: runner.Literal("new-label")}}},
	}
	response, err := p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, answer := range response.Answers {
		if _, ok := answer.Resolution.(runner.NoMatch); !ok {
			t.Fatalf("independent label recognition weakened conjunction/group: %#v", answer)
		}
	}
	originalDataset := response.Answers[2].DatasetStatus.DatasetID
	request.Snapshot.Content = strings.ReplaceAll(request.Snapshot.Content, "buildkite-windows", "new-label")
	response, err = p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.Answers[2].Resolution.(runner.Resolved); !ok {
		t.Fatal("snapshot update required rebuilding provider or host")
	}
	if response.Answers[2].DatasetStatus.DatasetID == originalDataset {
		t.Fatal("updated catalogue reused the old dataset identity")
	}
}

func TestRunnerProviderMissingAndEmptyCatalogue(t *testing.T) {
	t.Parallel()
	p := loadRunnerFixture(t)
	request := runnerFixtureRequest(t)
	request.Selectors = request.Selectors[:1]
	request.Snapshot.Content = ""
	response, err := p.ResolveRunners(t.Context(), request)
	if err != nil || len(response.Answers) != 1 {
		t.Fatalf("missing catalogue: %v %#v", err, response.Answers)
	}
	if _, unknown := response.Answers[0].Resolution.(runner.Unknown); !unknown || response.Answers[0].DatasetStatus.Freshness != runner.Missing || response.Answers[0].DatasetStatus.Complete {
		t.Fatalf("missing catalogue claimed knowledge: %#v", response.Answers[0])
	}
	request.Snapshot.Content = `{"queues":[]}`
	response, err = p.ResolveRunners(t.Context(), request)
	if err != nil || len(response.Answers) != 1 {
		t.Fatalf("empty catalogue: %v %#v", err, response.Answers)
	}
	if _, absent := response.Answers[0].Resolution.(runner.NoMatch); !absent || !response.Answers[0].DatasetStatus.Complete {
		t.Fatalf("exhaustive empty catalogue did not establish absence: %#v", response.Answers[0])
	}
}

func TestRunnerProviderFailureKeepsIndependentChecks(t *testing.T) {
	t.Parallel()
	p := loadRunnerFixture(t)
	snapshot := runnerFixtureRequest(t).Snapshot
	snapshot.Content = "{"
	observer := &runtimeObserver{}
	result, err := checking.Analyze(t.Context(), checking.Options{
		Runners:   runner.Options{Target: "buildkite", Scope: "fixture", Snapshot: snapshot, Providers: []runner.Registration{{Provider: p, ImplementationID: p.ArtifactDigest()}}},
		Analyzers: []checkinputs.Registration{{Checker: observer}},
	}, []workspace.Source{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    needs: missing\n    runs-on: buildkite-windows\n    steps: [{run: echo hello}]\n")}})
	if err == nil || !strings.Contains(err.Error(), "invalid queue snapshot") {
		t.Fatalf("missing operational error: %v", err)
	}
	if result == nil {
		t.Fatal("independent findings discarded")
	}
	independent := false
	for _, d := range result.Diagnostics {
		if d.Rule == "runner-label" {
			t.Fatal("operational failure became invalid runner", d)
		}
		independent = independent || strings.Contains(d.Message, "missing")
	}
	if !independent || len(observer.scripts) != 1 || observer.scripts[0].Runtime == nil || observer.scripts[0].Runtime.ShellKnown {
		t.Fatalf("independent analysis or uncertainty lost: %#v", result.Diagnostics)
	}
}

func TestRunnerProviderShellValidation(t *testing.T) {
	t.Parallel()
	p := loadRunnerFixture(t)
	for _, tc := range []struct {
		label string
		want  bool
	}{{"buildkite-windows", false}, {"buildkite-linux", true}} {
		result, err := checking.Analyze(t.Context(), checking.Options{
			Runners: runner.Options{Target: "buildkite", Scope: "fixture", Snapshot: runnerFixtureRequest(t).Snapshot, Providers: []runner.Registration{{Provider: p, ImplementationID: p.ArtifactDigest()}}},
		}, []workspace.Source{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: " + tc.label + "\n    steps: [{run: echo hello, shell: cmd}]\n")}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range result.Diagnostics {
			found = found || d.Rule == "shell-name"
		}
		if found != tc.want {
			t.Fatalf("%s: %#v", tc.label, result.Diagnostics)
		}
	}
}
