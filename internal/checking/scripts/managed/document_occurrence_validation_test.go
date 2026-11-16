package managed

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/scripts"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
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
	for name, change := range map[string]func(*scripts.Unit){
		"occurrence":  func(unit *scripts.Unit) { unit.Occurrence = "foreign" },
		"declaration": func(unit *scripts.Unit) { unit.Reference.Node = "foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			batch := documentBatchFixture(t, 1)
			change(&batch.Scripts[0])
			guest := &negotiationObserver{}
			runner := documentRunner{
				documentRoute: shellcheckRoute, guest: guest, integrator: guest,
				context: contract.Context{Document: checkinputs.AnalyzerInput{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}},
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
		runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: contract.Context{Document: checkinputs.AnalyzerInput{Reference: reference}}}
		_, err := runner.Analyze(t.Context(), batch)
		if !errors.Is(err, checkinputs.ErrInvalidCallGraph) || guest.calls != 0 {
			t.Fatalf("mismatched or absent header reached negotiation: calls=%d err=%v", guest.calls, err)
		}
	}
}

func TestDocumentBatchHeaderRequiredForSkippedInputs(t *testing.T) {
	t.Parallel()
	batch := documentBatchFixture(t, 1)
	graph := invocationfixture.Graph(t, invocationfixture.Script{Node: "run0", Input: "script", Entry: true})
	batch.CallGraph = graph
	batch.Scripts[0].Occurrence = graph.Scripts()[0].ID
	batch.Scripts[0].Reference = graph.Scripts()[0].Declaration
	batch.Scripts[0].Run, batch.Scripts[0].Execution = nil, nil
	batch.Scripts[0].Input = &scripts.ActionInput{
		Site:  checkinputs.ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"},
		Value: &definition.ActionArgument{Name: &definition.String{Value: "script"}, Value: &definition.String{Value: "echo test"}},
	}
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
