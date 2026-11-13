package executor

import (
	"context"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/inputs/invocations"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRunPreservesNilAndEmptyScriptSelection(t *testing.T) {
	t.Parallel()
	entry := source.Reference{Document: "workflow.yml", Revision: "r", Node: "root"}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	ownerGraph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	projection, err := invocations.New(ownerGraph)
	if err != nil {
		t.Fatal(err)
	}
	graph := projection.Graph()
	if !graph.Valid() {
		t.Fatal("empty invocation graph was not projected")
	}
	for _, tc := range []struct {
		name      string
		available []rulemeta.InputKind
		selection []checkinputs.OccurrenceID
		wantNil   bool
	}{
		{name: "nil", available: []rulemeta.InputKind{rulemeta.Invocations}, wantNil: true},
		{name: "empty nonnil", available: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}, selection: []checkinputs.OccurrenceID{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := checkinputs.Input{Reference: entry, AvailableInputs: tc.available, CallGraph: graph, SelectedScripts: tc.selection}
			registration := checkinputs.Registration{Checker: testAnalyzer{
				descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "selection-clone"}},
				run: func(_ context.Context, supplied checkinputs.Input) ([]results.Diagnostic, error) {
					if (supplied.SelectedScripts == nil) != tc.wantNil {
						t.Errorf("selected scripts nil=%v, want %v", supplied.SelectedScripts == nil, tc.wantNil)
					}
					return nil, nil
				},
			}}
			if _, err := Run(t.Context(), input, []checkinputs.Registration{registration}); err != nil {
				t.Fatal(err)
			}
			if (input.SelectedScripts == nil) != tc.wantNil {
				t.Fatalf("host input selection nil=%v, want %v", input.SelectedScripts == nil, tc.wantNil)
			}
		})
	}
}
