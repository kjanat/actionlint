package application_test

import (
	"context"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
)

type callGraphCollector struct {
	mu     sync.Mutex
	inputs []analyzer.Input
}

func (*callGraphCollector) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "call-graph", Requires: []analyzer.Fact{analyzer.Invocations, analyzer.Scripts}}
}

func (collector *callGraphCollector) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.inputs = append(collector.inputs, input)
	return nil, nil
}

func TestAnalyzerReceivesDistinctNestedOccurrences(t *testing.T) {
	root, _ := executableFixture(t)
	workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}, {uses: $/outer}]}}\n"
	first := writeFixture(t, root, "one.yml", workflow)
	second := writeFixture(t, root, "two.yml", workflow)
	writeFixture(t, root, "outer/action.yml", "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/middle}]}\n")
	writeFixture(t, root, "middle/action.yml", "name: middle\ndescription: middle\nruns: {using: composite, steps: [{uses: $/inner}]}\n")
	writeFixture(t, root, "inner/action.yml", "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo nested}]}\n")
	collector := &callGraphCollector{}
	_, err := application.Check(t.Context(), application.Options{WorkingDir: root, Analyzers: []analyzer.Registration{{Analyzer: collector}}}, application.Input{Kind: application.Files, Paths: []string{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if len(collector.inputs) != 2 {
		t.Fatalf("received %d inputs", len(collector.inputs))
	}
	seen := map[analyzer.OccurrenceID]bool{}
	for _, input := range collector.inputs {
		if err := input.ValidateInvocations(); err != nil {
			t.Fatal(err)
		}
		if len(input.Invoked) != 2 || len(input.CallGraph.Calls) != 6 {
			t.Fatalf("incomplete nested input: %+v", input.CallGraph)
		}
		left, right := input.Invoked[0], input.Invoked[1]
		if left.Reference != right.Reference || left.Occurrence.Owner == right.Occurrence.Owner || left.Occurrence.Ancestry[1] == right.Occurrence.Ancestry[1] {
			t.Fatal("shared declaration lost distinct outer calls")
		}
		for _, script := range input.Invoked {
			occurrence := script.Occurrence
			if occurrence.Depth != 3 || len(occurrence.Ancestry) != 4 || occurrence.Ancestry[0] != input.CallGraph.Entry.ID || occurrence.Ancestry[3] != occurrence.Owner || seen[occurrence.ID] {
				t.Fatalf("lost occurrence identity across entries: %+v", occurrence)
			}
			seen[occurrence.ID] = true
		}
	}
	if len(seen) != 4 {
		t.Fatal("distinct executions collapsed")
	}
}
