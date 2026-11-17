package checking_test

import (
	"context"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

type callGraphCollector struct {
	mu     sync.Mutex
	inputs []checkinputs.AnalyzerInput
}

func (*callGraphCollector) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "call-graph", Category: "correctness", RequiredInputs: []rule.InputKind{rule.Invocations, rule.Scripts}}
}

func (collector *callGraphCollector) CheckDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
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
	_, err := checking.Check(t.Context(), checking.Options{WorkingDir: root, Analyzers: []checkinputs.Registration{{Checker: collector}}}, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if len(collector.inputs) != 2 {
		t.Fatalf("received %d inputs", len(collector.inputs))
	}
	seen := map[invocation.ID]bool{}
	for _, input := range collector.inputs {
		if err := input.ValidateInvocations(); err != nil {
			t.Fatal(err)
		}
		if len(input.Invoked) != 2 || input.CallGraph.CallCount() != 6 {
			t.Fatalf("incomplete nested input: %+v", input.CallGraph.Calls())
		}
		left, right := input.Invoked[0], input.Invoked[1]
		leftChain, _ := input.CallGraph.Chain(left.Occurrence.Owner)
		rightChain, _ := input.CallGraph.Chain(right.Occurrence.Owner)
		if left.Reference != right.Reference || left.Occurrence.Owner == right.Occurrence.Owner || leftChain[1] == rightChain[1] {
			t.Fatal("shared declaration lost distinct outer calls")
		}
		for _, script := range input.Invoked {
			occurrence := script.Occurrence
			chain, _ := input.CallGraph.Chain(occurrence.Owner)
			if len(chain) != 4 || chain[0] != input.CallGraph.RootID() || chain[3] != occurrence.Owner || seen[occurrence.ID] {
				t.Fatalf("lost occurrence identity across entries: %+v", occurrence)
			}
			seen[occurrence.ID] = true
		}
	}
	if len(seen) != 4 {
		t.Fatal("distinct executions collapsed")
	}
}
