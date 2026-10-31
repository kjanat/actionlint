package analysis_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/runner"
)

type orderedEvidenceProvider struct {
	waitFor string
	release <-chan struct{}
}

func (*orderedEvidenceProvider) RunnerDescriptor() runner.Descriptor {
	return runner.Descriptor{Name: "order", Version: "1", Targets: []string{"order"}}
}

func (p *orderedEvidenceProvider) ResolveRunners(ctx context.Context, request runner.Request) ([]runner.Answer, error) {
	label := fmt.Sprint(request.Selectors[0].Labels[0].Value)
	if label == p.waitFor && p.release != nil {
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []runner.Answer{{Resolution: runner.Unknown{Reason: label}, Evidence: runner.Evidence{Dataset: label, Scope: request.Scope, Freshness: runner.Fresh, Complete: true}}}, nil
}

type evidenceCompletionObserver struct {
	label   string
	release chan struct{}
	once    sync.Once
}

func (*evidenceCompletionObserver) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "evidence-completion", Requires: []analyzer.Fact{analyzer.Source}}
}

func (o *evidenceCompletionObserver) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	if strings.Contains(input.Content, "runs-on: "+o.label) {
		o.once.Do(func() { close(o.release) })
	}
	return nil, nil
}

// One path holds one source revision: differing content under one path is a
// capture conflict, so reports order by path. The source that sorts first
// finishes last, and completion order must not leak into the evidence.
func TestRunnerEvidenceOrderIgnoresCompletion(t *testing.T) {
	root := t.TempDir()
	const slow, fast = "alpha", "beta"
	source := func(path, label string) analysis.Source {
		return analysis.Source{Path: path, Content: []byte("on: push\njobs:\n  test:\n    runs-on: " + label + "\n    steps: [{run: echo ok}]\n")}
	}
	want := []string{"a.yml", "b.yml"}
	for range 2 {
		release := make(chan struct{})
		observer := &evidenceCompletionObserver{label: fast, release: release}
		settings := analysis.Options{
			WorkingDir: root,
			Runners:    runner.Options{Target: "order", Providers: []runner.Registration{{Provider: &orderedEvidenceProvider{waitFor: slow, release: release}, Identity: "same-provider"}}},
			Analyzers:  []analyzer.Registration{{Analyzer: observer}},
		}
		// a.yml waits until b.yml has been analyzed.
		result, err := analysis.Analyze(t.Context(), analysis.Request{Sources: []analysis.Source{source("b.yml", fast), source("a.yml", slow)}, Options: settings})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, report := range result.RunnerEvidence {
			got = append(got, report.Path)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("completion order leaked into evidence: got=%v want=%v", got, want)
		}
	}
}
