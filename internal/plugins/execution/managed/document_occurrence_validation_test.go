package managed

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/input/source"
)

type negotiationObserver struct {
	scalingGuest
	calls int
}

func (guest *negotiationObserver) NegotiateTool(ctx context.Context, data, version json.RawMessage) (*checkinputs.ToolNegotiation, error) {
	guest.calls++
	return guest.fallbackGuest.NegotiateTool(ctx, data, version)
}

func TestDocumentBatchRejectsAlteredOccurrenceBeforeNegotiation(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*checkinputs.ScriptOccurrence){
		"owner":    func(value *checkinputs.ScriptOccurrence) { value.Owner = "foreign" },
		"ancestry": func(value *checkinputs.ScriptOccurrence) { value.Ancestry[0] = "foreign" },
		"depth":    func(value *checkinputs.ScriptOccurrence) { value.Depth++ },
		"input":    func(value *checkinputs.ScriptOccurrence) { value.Input = "script" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			batch := documentBatchFixture(t, 1)
			change(&batch.Scripts[0].Fact.Occurrence)
			guest := &negotiationObserver{}
			runner := documentRunner{
				documentRoute: shellcheckRoute, guest: guest, integrator: guest,
				context: tools.Context{Document: checkinputs.Input{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}},
			}
			_, err := runner.Analyze(t.Context(), batch)
			if !errors.Is(err, checkinputs.ErrInvalidCallGraph) {
				t.Fatalf("altered occurrence accepted: %v", err)
			}
			if guest.calls != 0 {
				t.Fatalf("invalid occurrence reached %d negotiation callbacks", guest.calls)
			}
		})
	}
}

func TestDocumentBatchRejectsMismatchedHeaderBeforeNegotiation(t *testing.T) {
	t.Parallel()
	batch := documentBatchFixture(t, 1)
	wrong := batch.CallGraph.Entry().Reference
	wrong.Revision = "wrong"
	for _, reference := range []source.Reference{{}, wrong} {
		guest := &negotiationObserver{}
		runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: tools.Context{Document: checkinputs.Input{Reference: reference}}}
		_, err := runner.Analyze(t.Context(), batch)
		if !errors.Is(err, checkinputs.ErrInvalidCallGraph) || guest.calls != 0 {
			t.Fatalf("mismatched or absent header reached negotiation: calls=%d err=%v", guest.calls, err)
		}
	}
}

func TestDocumentBatchHeaderRequiredForSkippedInputs(t *testing.T) {
	t.Parallel()
	batch := documentBatchFixture(t, 1)
	records := batch.CallGraph.Export()
	records.Scripts[0].Kind = checkinputs.ActionInputScript
	records.Scripts[0].Input = "script"
	graph, err := checkinputs.NewCallGraph(records)
	if err != nil {
		t.Fatal(err)
	}
	batch.CallGraph = graph
	batch.Scripts[0].Fact.Occurrence = records.Scripts[0]
	batch.Scripts[0].Fact.Runtime = nil
	batch.Scripts[0].Input = &checkinputs.ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"}
	guest := &negotiationObserver{}
	runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest}
	if _, err := runner.Analyze(t.Context(), batch); !errors.Is(err, checkinputs.ErrInvalidCallGraph) {
		t.Fatalf("skipped inputs accepted absent header: %v", err)
	}
	runner.context.Document.Reference = graph.Entry().Reference
	if _, err := runner.Analyze(t.Context(), batch); err != nil {
		t.Fatalf("matching skipped-input batch failed: %v", err)
	}
	if guest.calls != 0 {
		t.Fatal("action input reached ShellCheck negotiation")
	}
}
