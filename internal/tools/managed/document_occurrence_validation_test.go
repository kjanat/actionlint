package managed

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/source"
)

type negotiationObserver struct {
	scalingGuest
	calls int
}

func (guest *negotiationObserver) NegotiateTool(ctx context.Context, data, version json.RawMessage) (*analyzer.ToolNegotiation, error) {
	guest.calls++
	return guest.fallbackGuest.NegotiateTool(ctx, data, version)
}

func TestDocumentBatchRejectsAlteredOccurrenceBeforeNegotiation(t *testing.T) {
	for name, change := range map[string]func(*analyzer.ScriptOccurrence){
		"owner":    func(value *analyzer.ScriptOccurrence) { value.Owner = "foreign" },
		"ancestry": func(value *analyzer.ScriptOccurrence) { value.Ancestry[0] = "foreign" },
		"depth":    func(value *analyzer.ScriptOccurrence) { value.Depth++ },
		"input":    func(value *analyzer.ScriptOccurrence) { value.Input = "script" },
	} {
		t.Run(name, func(t *testing.T) {
			batch := documentBatchFixture(t, 1)
			change(&batch.Scripts[0].Fact.Occurrence)
			guest := &negotiationObserver{}
			runner := documentRunner{
				documentRoute: shellcheckRoute, guest: guest, integrator: guest,
				context: tools.Context{Document: analyzer.Input{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}},
			}
			_, err := runner.Analyze(t.Context(), batch)
			if !errors.Is(err, analyzer.ErrInvalidCallGraph) {
				t.Fatalf("altered occurrence accepted: %v", err)
			}
			if guest.calls != 0 {
				t.Fatalf("invalid occurrence reached %d negotiation callbacks", guest.calls)
			}
		})
	}
}

func TestDocumentBatchRejectsMismatchedHeaderBeforeNegotiation(t *testing.T) {
	batch := documentBatchFixture(t, 1)
	wrong := batch.CallGraph.Entry().Reference
	wrong.Revision = "wrong"
	for _, reference := range []source.Reference{{}, wrong} {
		guest := &negotiationObserver{}
		runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: tools.Context{Document: analyzer.Input{Reference: reference}}}
		_, err := runner.Analyze(t.Context(), batch)
		if !errors.Is(err, analyzer.ErrInvalidCallGraph) || guest.calls != 0 {
			t.Fatalf("mismatched or absent header reached negotiation: calls=%d err=%v", guest.calls, err)
		}
	}
}

func TestDocumentBatchHeaderRequiredForSkippedInputs(t *testing.T) {
	batch := documentBatchFixture(t, 1)
	records := batch.CallGraph.Snapshot()
	records.Scripts[0].Kind = analyzer.ActionInputScript
	records.Scripts[0].Input = "script"
	graph, err := analyzer.NewCallGraph(records)
	if err != nil {
		t.Fatal(err)
	}
	batch.CallGraph = graph
	batch.Scripts[0].Fact.Occurrence = records.Scripts[0]
	batch.Scripts[0].Fact.Runtime = nil
	batch.Scripts[0].Input = &analyzer.ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"}
	guest := &negotiationObserver{}
	runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest}
	if _, err := runner.Analyze(t.Context(), batch); !errors.Is(err, analyzer.ErrInvalidCallGraph) {
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
