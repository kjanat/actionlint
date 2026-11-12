package checking_test

import (
	"context"
	"sync"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/results"
)

type callGraphCollector struct {
	mu     sync.Mutex
	inputs []checkinputs.Input
}

func (*callGraphCollector) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "call-graph", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}}
}

func (collector *callGraphCollector) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.inputs = append(collector.inputs, input)
	return nil, nil
}

func TestAnalyzerReceivesDistinctNestedOccurrences(t *testing.T) {
	t.Parallel()
	root, _ := executableFixture(t)
	workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}, {uses: $/outer}]}}\n"
	first := writeFixture(t, root, "one.yml", workflow)
	second := writeFixture(t, root, "two.yml", workflow)
	writeFixture(t, root, "outer/action.yml", "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/middle}]}\n")
	writeFixture(t, root, "middle/action.yml", "name: middle\ndescription: middle\nruns: {using: composite, steps: [{uses: $/inner}]}\n")
	writeFixture(t, root, "inner/action.yml", "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo nested}]}\n")
	collector := &callGraphCollector{}
	_, err := checking.Check(t.Context(), checking.Options{WorkingDir: root, Analyzers: []checkinputs.Registration{{Checker: collector}}}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if len(collector.inputs) != 2 {
		t.Fatalf("received %d inputs", len(collector.inputs))
	}
	seen := map[checkinputs.OccurrenceID]bool{}
	for _, input := range collector.inputs {
		if err := input.ValidateInvocations(); err != nil {
			t.Fatal(err)
		}
		graph := input.CallGraph.Export()
		if len(input.Invoked) != 2 || input.CallGraph.CallCount() != 6 {
			t.Fatalf("incomplete nested input: %+v", graph)
		}
		left, right := input.Invoked[0], input.Invoked[1]
		if left.Reference != right.Reference || left.Occurrence.Owner == right.Occurrence.Owner || left.Occurrence.Ancestry[1] == right.Occurrence.Ancestry[1] {
			t.Fatal("shared declaration lost distinct outer calls")
		}
		for _, script := range input.Invoked {
			occurrence := script.Occurrence
			if occurrence.Depth != 3 || len(occurrence.Ancestry) != 4 || occurrence.Ancestry[0] != input.CallGraph.Entry().ID || occurrence.Ancestry[3] != occurrence.Owner || seen[occurrence.ID] {
				t.Fatalf("lost occurrence identity across entries: %+v", occurrence)
			}
			seen[occurrence.ID] = true
		}
	}
	if len(seen) != 4 {
		t.Fatal("distinct executions collapsed")
	}
}
