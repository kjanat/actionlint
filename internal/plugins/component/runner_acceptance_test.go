package component

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/runner"
)

func TestRunnerProviderConjunctionAndRefresh(t *testing.T) {
	p := loadRunnerFixture(t)
	request := runnerFixtureRequest(t)
	request.Selectors = []runner.Selector{
		{Labels: []runner.Constraint{{Value: runner.Literal("buildkite-windows")}, {Value: runner.Literal("buildkite-linux")}}},
		{Labels: []runner.Constraint{{Value: runner.Literal("buildkite-windows")}}, Group: &runner.Constraint{Value: runner.Literal("wrong")}},
		{Labels: []runner.Constraint{{Value: runner.Literal("new-label")}}},
	}
	answers, err := p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, answer := range answers {
		if _, ok := answer.Resolution.(runner.NoMatch); !ok {
			t.Fatalf("independent label recognition weakened conjunction/group: %#v", answer)
		}
	}
	originalDataset := answers[2].Evidence.Dataset
	request.Snapshot.Content = strings.ReplaceAll(request.Snapshot.Content, "buildkite-windows", "new-label")
	answers, err = p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := answers[2].Resolution.(runner.Resolved); !ok {
		t.Fatal("snapshot update required rebuilding provider or host")
	}
	if answers[2].Evidence.Dataset == originalDataset {
		t.Fatal("updated catalogue reused the old dataset identity")
	}
}

func TestRunnerProviderMissingAndEmptyCatalogue(t *testing.T) {
	p := loadRunnerFixture(t)
	request := runnerFixtureRequest(t)
	request.Selectors = request.Selectors[:1]
	request.Snapshot.Content = ""
	answers, err := p.ResolveRunners(t.Context(), request)
	if err != nil || len(answers) != 1 {
		t.Fatalf("missing catalogue: %v %#v", err, answers)
	}
	if _, unknown := answers[0].Resolution.(runner.Unknown); !unknown || answers[0].Evidence.Freshness != runner.Missing || answers[0].Evidence.Complete {
		t.Fatalf("missing catalogue claimed knowledge: %#v", answers[0])
	}
	request.Snapshot.Content = `{"queues":[]}`
	answers, err = p.ResolveRunners(t.Context(), request)
	if err != nil || len(answers) != 1 {
		t.Fatalf("empty catalogue: %v %#v", err, answers)
	}
	if _, absent := answers[0].Resolution.(runner.NoMatch); !absent || !answers[0].Evidence.Complete {
		t.Fatalf("exhaustive empty catalogue did not establish absence: %#v", answers[0])
	}
}

func TestRunnerProviderFailureKeepsIndependentChecks(t *testing.T) {
	p := loadRunnerFixture(t)
	snapshot := runnerFixtureRequest(t).Snapshot
	snapshot.Content = "{"
	observer := &runtimeObserver{}
	result, err := analysis.Analyze(t.Context(), analysis.Request{Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    needs: missing\n    runs-on: buildkite-windows\n    steps: [{run: echo hello}]\n")}}, Options: analysis.Options{
		Runners:   runner.Options{Target: "buildkite", Scope: "fixture", Snapshot: snapshot, Providers: []runner.Registration{{Provider: p, Identity: p.ArtifactDigest()}}},
		Analyzers: []analyzer.Registration{{Analyzer: observer}},
	}})
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
	p := loadRunnerFixture(t)
	for _, tc := range []struct {
		label string
		want  bool
	}{{"buildkite-windows", false}, {"buildkite-linux", true}} {
		result, err := analysis.Analyze(t.Context(), analysis.Request{Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: " + tc.label + "\n    steps: [{run: echo hello, shell: cmd}]\n")}}, Options: analysis.Options{
			Runners: runner.Options{Target: "buildkite", Scope: "fixture", Snapshot: runnerFixtureRequest(t).Snapshot, Providers: []runner.Registration{{Provider: p, Identity: p.ArtifactDigest()}}},
		}})
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
