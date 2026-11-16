package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
	"actionlint.kjanat.dev/v2/internal/results"
)

type testAnalyzer struct {
	descriptor inputs.Descriptor
	run        func(context.Context, inputs.AnalyzerInput) ([]results.Diagnostic, error)
}

func (a testAnalyzer) Descriptor() inputs.Descriptor { return a.descriptor }
func (a testAnalyzer) CheckDocument(ctx context.Context, input inputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return a.run(ctx, input)
}

func TestRunIsolatesFactsAndRetainsPartialResults(t *testing.T) {
	t.Parallel()
	failure := errors.New("incomplete")
	input := inputs.AnalyzerInput{Path: "workflow.yml", AvailableInputs: []rule.InputKind{rule.Source, rule.Mappings}, Mappings: []facts.Mapping{{Entries: []mapping.Declaration{{Name: "original"}}}}}
	changed := make(chan struct{})
	input.SourceRoots = []inputs.SourceRoot{{Host: "/source", Guest: "/workspace"}}
	first := testAnalyzer{inputs.Descriptor{Name: "first", RequiredInputs: []rule.InputKind{rule.Mappings}}, func(_ context.Context, in inputs.AnalyzerInput) ([]results.Diagnostic, error) {
		in.Mappings[0].Entries[0].Name = "changed"
		in.SourceRoots[0].Guest = "/changed"
		close(changed)
		return []results.Diagnostic{{Message: "partial"}}, failure
	}}
	second := testAnalyzer{inputs.Descriptor{Name: "second", RequiredInputs: []rule.InputKind{rule.Mappings}}, func(_ context.Context, in inputs.AnalyzerInput) ([]results.Diagnostic, error) {
		<-changed
		if in.Mappings[0].Entries[0].Name != "original" {
			t.Error("shared mutable facts")
		}
		if in.SourceRoots[0].Guest != "/workspace" {
			t.Error("shared mutable source roots")
		}
		return []results.Diagnostic{{Message: "complete"}}, nil
	}}
	findings, err := Run(t.Context(), input, []inputs.Registration{{Checker: first}, {Checker: second}})
	if !errors.Is(err, failure) || len(findings) != 2 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
	if findings[0].Rule != "first" || findings[1].Rule != "second" || findings[0].Path != input.Path {
		t.Fatal(findings)
	}
	if input.Mappings[0].Entries[0].Name != "original" {
		t.Fatal("input modified")
	}
}

func TestRunRequirementsAndCancellation(t *testing.T) {
	t.Parallel()
	for _, requirement := range []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution, "future"} {
		a := testAnalyzer{inputs.Descriptor{Name: "requires", RequiredInputs: []rule.InputKind{requirement}}, func(context.Context, inputs.AnalyzerInput) ([]results.Diagnostic, error) {
			t.Error("ran without required facts")
			return nil, nil
		}}
		_, err := Run(t.Context(), inputs.AnalyzerInput{}, []inputs.Registration{{Checker: a}})
		if err == nil || !strings.Contains(err.Error(), string(requirement)) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Run(ctx, inputs.AnalyzerInput{}, []inputs.Registration{{}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRunPreparesIsolatedFactsAndPreservesPartialFindings(t *testing.T) {
	t.Parallel()
	failure := errors.New("second lookup failed")
	input := inputs.AnalyzerInput{
		Path: "workflow.yml", AvailableInputs: []rule.InputKind{rule.Dependencies},
		Dependencies:         []facts.Dependency{{Uses: "original/repo@v1"}},
		ResolvedDependencies: []facts.ResolvedDependency{{Commit: "original"}},
	}
	checker := testAnalyzer{inputs.Descriptor{Name: "prepared", RequiredInputs: []rule.InputKind{rule.Dependencies}}, func(_ context.Context, supplied inputs.AnalyzerInput) ([]results.Diagnostic, error) {
		if supplied.Dependencies[0].Uses != "prepared/repo@v1" || supplied.ResolvedDependencies[0].Commit != "resolved" {
			t.Fatal("checker ran without prepared facts")
		}
		return []results.Diagnostic{{Message: "resolved first dependency"}}, nil
	}}
	registration := inputs.Registration{Checker: checker, Prepare: func(_ context.Context, supplied inputs.AnalyzerInput) (inputs.AnalyzerInput, error) {
		supplied.Dependencies[0].Uses = "prepared/repo@v1"
		supplied.ResolvedDependencies[0].Commit = "resolved"
		return supplied, failure
	}}
	findings, err := Run(t.Context(), input, []inputs.Registration{registration})
	if !errors.Is(err, failure) || len(findings) != 1 {
		t.Fatalf("partial preparation lost findings: %v %v", findings, err)
	}
	if input.Dependencies[0].Uses != "original/repo@v1" || input.ResolvedDependencies[0].Commit != "original" {
		t.Fatal("preparation changed caller-owned facts")
	}
}

func TestRunCancellationDuringPreparationDoesNotStartChecker(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checker := testAnalyzer{inputs.Descriptor{Name: "cancelled"}, func(context.Context, inputs.AnalyzerInput) ([]results.Diagnostic, error) {
		t.Fatal("checker started after preparation was canceled")
		return nil, nil
	}}
	registration := inputs.Registration{Checker: checker, Prepare: func(_ context.Context, input inputs.AnalyzerInput) (inputs.AnalyzerInput, error) {
		cancel()
		return input, nil
	}}
	if _, err := Run(ctx, inputs.AnalyzerInput{}, []inputs.Registration{registration}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation changed: %v", err)
	}
}

func TestRunIsolatesActionInputFacts(t *testing.T) {
	t.Parallel()
	graph := invocationfixture.Graph(t, invocationfixture.Script{Node: "script", Input: "script"})
	occurrence := graph.Scripts()[0]
	declaration := occurrence.Declaration
	input := inputs.AnalyzerInput{Reference: graph.Entry().Reference, AvailableInputs: []rule.InputKind{rule.Scripts, rule.Invocations}, CallGraph: graph, ActionInputs: []inputs.ActionInput{{
		Reference: declaration,
		Spans:     []sourcemap.Span{{End: 8}}, Occurrence: occurrence,
		Site: inputs.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"},
	}}}
	changed := make(chan struct{})
	first := testAnalyzer{inputs.Descriptor{Name: "first", RequiredInputs: []rule.InputKind{rule.Scripts}}, func(_ context.Context, in inputs.AnalyzerInput) ([]results.Diagnostic, error) {
		in.ActionInputs[0].Site.Language = "python"
		in.ActionInputs[0].Spans[0].End = 99
		in.ActionInputs[0].Occurrence.Owner = "changed"
		close(changed)
		return nil, nil
	}}
	second := testAnalyzer{inputs.Descriptor{Name: "second", RequiredInputs: []rule.InputKind{rule.Scripts}}, func(_ context.Context, in inputs.AnalyzerInput) ([]results.Diagnostic, error) {
		<-changed
		got := in.ActionInputs[0]
		if got.Site.Language != "javascript" || got.Spans[0].End != 8 || got.Occurrence != occurrence {
			t.Errorf("action input facts shared between analyzers: %+v", got)
		}
		return nil, nil
	}}
	if _, err := Run(t.Context(), input, []inputs.Registration{{Checker: first}, {Checker: second}}); err != nil {
		t.Fatal(err)
	}
}
