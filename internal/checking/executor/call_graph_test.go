package executor

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRunIsolatesCallGraphAndOccurrenceValues(t *testing.T) {
	t.Parallel()
	graph := invocationfixture.Graph(t, invocationfixture.Script{Node: "run"})
	occurrence := graph.Scripts()[0]
	input := checkinputs.AnalyzerInput{
		Reference: graph.Entry().Reference, CallGraph: graph, AvailableInputs: []rule.InputKind{rule.Invocations, rule.Scripts},
		Invoked: []checkinputs.Script{{Reference: occurrence.Declaration, Occurrence: occurrence}}, SelectedScripts: []invocation.ID{occurrence.ID},
	}
	changed := make(chan struct{})
	first := testAnalyzer{checkinputs.Descriptor{Name: "mutator"}, func(_ context.Context, in checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
		in.SelectedScripts[0] = "changed"
		in.Invoked[0].Occurrence.Owner = "changed"
		close(changed)
		return nil, nil
	}}
	second := testAnalyzer{checkinputs.Descriptor{Name: "observer"}, func(_ context.Context, in checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
		<-changed
		if len(in.SelectedScripts) != 1 || in.SelectedScripts[0] != occurrence.ID || in.Invoked[0].Occurrence != occurrence || !in.CallGraph.Valid() {
			t.Error("analyzer mutation reached sibling input")
		}
		return nil, nil
	}}
	if _, err := Run(t.Context(), input, []results.AnalyzerRegistration{{Analyzer: first}, {Analyzer: second}}); err != nil {
		t.Fatal(err)
	}
	if input.SelectedScripts[0] != occurrence.ID || input.Invoked[0].Occurrence != occurrence {
		t.Fatal("analyzer mutation changed the host input")
	}
}
