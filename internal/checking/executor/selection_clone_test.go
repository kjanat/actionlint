package executor

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRunPreservesNilAndEmptyScriptSelection(t *testing.T) {
	t.Parallel()
	entry := sourcetext.Reference{Document: "workflow.yml", Revision: "r", Node: "root"}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		available []rule.InputKind
		selection []invocation.ID
		wantNil   bool
	}{
		{name: "nil", available: []rule.InputKind{rule.Invocations}, wantNil: true},
		{name: "empty nonnil", available: []rule.InputKind{rule.Invocations, rule.Scripts}, selection: []invocation.ID{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := checkinputs.AnalyzerInput{Reference: entry, AvailableInputs: tc.available, CallGraph: graph, SelectedScripts: tc.selection}
			registration := checkinputs.Registration{Analyzer: testAnalyzer{
				descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "selection-clone"}},
				run: func(_ context.Context, supplied checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
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
