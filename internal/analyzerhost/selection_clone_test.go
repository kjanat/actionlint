package analyzerhost

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/invocationview"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/rule"
	"actionlint.kjanat.dev/v2/source"
)

func TestRunPreservesNilAndEmptyScriptSelection(t *testing.T) {
	entry := source.Reference{Document: "workflow.yml", Revision: "r", Node: "root"}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	ownerGraph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	projection, err := invocationview.New(ownerGraph)
	if err != nil {
		t.Fatal(err)
	}
	graph := projection.Snapshot()
	if graph == nil {
		t.Fatal("empty invocation graph was not projected")
	}
	for _, tc := range []struct {
		name      string
		available []analyzer.Fact
		selection []analyzer.OccurrenceID
		wantNil   bool
	}{
		{name: "nil", available: []analyzer.Fact{analyzer.Invocations}, wantNil: true},
		{name: "empty nonnil", available: []analyzer.Fact{analyzer.Invocations, analyzer.Scripts}, selection: []analyzer.OccurrenceID{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := analyzer.Input{Reference: entry, Available: tc.available, CallGraph: graph, SelectedScripts: tc.selection}
			registration := analyzer.Registration{Analyzer: testAnalyzer{
				descriptor: analyzer.Descriptor{Descriptor: rule.Descriptor{Name: "selection-clone"}},
				run: func(_ context.Context, supplied analyzer.Input) ([]diagnostic.Diagnostic, error) {
					if (supplied.SelectedScripts == nil) != tc.wantNil {
						t.Errorf("selected scripts nil=%v, want %v", supplied.SelectedScripts == nil, tc.wantNil)
					}
					return nil, nil
				},
			}}
			if _, err := Run(t.Context(), input, []analyzer.Registration{registration}); err != nil {
				t.Fatal(err)
			}
			if (input.SelectedScripts == nil) != tc.wantNil {
				t.Fatalf("host input selection nil=%v, want %v", input.SelectedScripts == nil, tc.wantNil)
			}
		})
	}
}
