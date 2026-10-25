package analyzerhost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

type testAnalyzer struct {
	descriptor analyzer.Descriptor
	run        func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error)
}

func (a testAnalyzer) Descriptor() analyzer.Descriptor { return a.descriptor }
func (a testAnalyzer) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return a.run(ctx, input)
}

func TestScopeJoinsAllResourcesOnce(t *testing.T) {
	failure := errors.New("resource failed")
	var calls atomic.Int32
	var scope Scope
	scope.Register(func() error { calls.Add(1); return failure })
	scope.Register(func() error { calls.Add(1); return nil })
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if err := scope.Finish(); !errors.Is(err, failure) {
				t.Errorf("Finish: %v", err)
			}
		})
	}
	group.Wait()
	if calls.Load() != 2 {
		t.Fatalf("joined %d resources", calls.Load())
	}
}

func TestRunIsolatesFactsAndRetainsPartialResults(t *testing.T) {
	failure := errors.New("incomplete")
	input := analyzer.Input{Path: "workflow.yml", Available: []analyzer.Fact{analyzer.Source, analyzer.Mappings}, Mappings: []analyzer.Mapping{{Entries: []analyzer.Entry{{Name: "original"}}}}}
	changed := make(chan struct{})
	input.SourceRoots = []analyzer.SourceRoot{{Host: "/source", Guest: "/workspace"}}
	first := testAnalyzer{analyzer.Descriptor{Name: "first", Requires: []analyzer.Fact{analyzer.Mappings}}, func(_ context.Context, in analyzer.Input) ([]diagnostic.Diagnostic, error) {
		in.Mappings[0].Entries[0].Name = "changed"
		in.SourceRoots[0].Guest = "/changed"
		close(changed)
		return []diagnostic.Diagnostic{{Message: "partial"}}, failure
	}}
	second := testAnalyzer{analyzer.Descriptor{Name: "second", Requires: []analyzer.Fact{analyzer.Mappings}}, func(_ context.Context, in analyzer.Input) ([]diagnostic.Diagnostic, error) {
		<-changed
		if in.Mappings[0].Entries[0].Name != "original" {
			t.Error("shared mutable facts")
		}
		if in.SourceRoots[0].Guest != "/workspace" {
			t.Error("shared mutable source roots")
		}
		return []diagnostic.Diagnostic{{Message: "complete"}}, nil
	}}
	findings, err := Run(t.Context(), input, []analyzer.Registration{{Analyzer: first}, {Analyzer: second}})
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
	for _, requirement := range []analyzer.Fact{analyzer.Scripts, analyzer.Invocations, analyzer.Execution, "future"} {
		a := testAnalyzer{analyzer.Descriptor{Name: "requires", Requires: []analyzer.Fact{requirement}}, func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
			t.Error("ran without required facts")
			return nil, nil
		}}
		_, err := Run(t.Context(), analyzer.Input{}, []analyzer.Registration{{Analyzer: a}})
		if err == nil || !strings.Contains(err.Error(), string(requirement)) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Run(ctx, analyzer.Input{}, []analyzer.Registration{{}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRunIsolatesActionInputFacts(t *testing.T) {
	input := analyzer.Input{Available: []analyzer.Fact{analyzer.Scripts}, ActionInputs: []analyzer.ActionInput{{
		Site:       analyzer.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"},
		Spans:      []analyzer.Span{{Line: 1, Start: 1, End: 8}},
		Invocation: &analyzer.Invocation{Depth: 1},
	}}}
	changed := make(chan struct{})
	first := testAnalyzer{analyzer.Descriptor{Name: "first", Requires: []analyzer.Fact{analyzer.Scripts}}, func(_ context.Context, in analyzer.Input) ([]diagnostic.Diagnostic, error) {
		in.ActionInputs[0].Site.Language = "python"
		in.ActionInputs[0].Spans[0].End = 99
		in.ActionInputs[0].Invocation.Depth = 99
		close(changed)
		return nil, nil
	}}
	second := testAnalyzer{analyzer.Descriptor{Name: "second", Requires: []analyzer.Fact{analyzer.Scripts}}, func(_ context.Context, in analyzer.Input) ([]diagnostic.Diagnostic, error) {
		<-changed
		got := in.ActionInputs[0]
		if got.Site.Language != "javascript" || got.Spans[0].End != 8 || got.Invocation.Depth != 1 {
			t.Errorf("action input facts shared between analyzers: %+v", got)
		}
		return nil, nil
	}}
	if _, err := Run(t.Context(), input, []analyzer.Registration{{Analyzer: first}, {Analyzer: second}}); err != nil {
		t.Fatal(err)
	}
}
