package analysis_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/runner"
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

func TestRunnerEvidenceOrdersDuplicatePathsByIdentity(t *testing.T) {
	root := t.TempDir()
	source := func(label string) analysis.Source {
		return analysis.Source{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  test:\n    runs-on: " + label + "\n    steps: [{run: echo ok}]\n")}
	}
	options := func(provider runner.Provider) analysis.Options {
		return analysis.Options{WorkingDir: root, Runners: runner.Options{Target: "order", Providers: []runner.Registration{{Provider: provider, Identity: "same-provider"}}}}
	}
	identities := make(map[string]string)
	for _, label := range []string{"alpha", "beta"} {
		result, err := analysis.Analyze(t.Context(), analysis.Request{Sources: []analysis.Source{source(label)}, Options: options(&orderedEvidenceProvider{})})
		if err != nil || len(result.RunnerEvidence) != 1 {
			t.Fatalf("seed evidence: result=%+v error=%v", result, err)
		}
		identities[label] = result.RunnerEvidence[0].Identity
	}
	slow, fast := "alpha", "beta"
	if identities[slow] > identities[fast] {
		slow, fast = fast, slow
	}
	want := []string{identities[slow], identities[fast]}
	for range 2 {
		release := make(chan struct{})
		observer := &evidenceCompletionObserver{label: fast, release: release}
		settings := options(&orderedEvidenceProvider{waitFor: slow, release: release})
		settings.Analyzers = []analyzer.Registration{{Analyzer: observer}}
		// The smaller identity waits until the larger report has been recorded.
		result, err := analysis.Analyze(t.Context(), analysis.Request{Sources: []analysis.Source{source(fast), source(slow)}, Options: settings})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, report := range result.RunnerEvidence {
			if report.Path != "workflow.yml" {
				t.Fatalf("unexpected source path: %s", report.Path)
			}
			got = append(got, report.Identity)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("completion order leaked into evidence: got=%v want=%v", got, want)
		}
	}
}
