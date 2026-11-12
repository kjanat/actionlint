package managed

import (
	"errors"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
)

func TestDocumentBatchRejectsMissingFactsBeforeNegotiation(t *testing.T) {
	t.Parallel()
	valid := documentBatchFixture(t, 1)
	missingFact := valid
	missingFact.Scripts = append([]tools.Script(nil), valid.Scripts...)
	missingFact.Scripts[0].Fact = checkinputs.Script{}
	foreign := valid
	foreign.Scripts = append([]tools.Script(nil), valid.Scripts...)
	foreign.Scripts[0].Fact.Occurrence.ID = "unknown"
	for name, batch := range map[string]tools.Batch{
		"missing graph": {Scripts: []tools.Script{{}}},
		"missing fact":  missingFact,
		"foreign fact":  foreign,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Nil callbacks prove invalid batches cannot reach negotiation or analysis.
			runner := documentRunner{documentRoute: shellcheckRoute, context: tools.Context{Document: checkinputs.Input{Reference: valid.CallGraph.Entry().Reference}}}
			_, err := runner.Analyze(t.Context(), batch)
			if !errors.Is(err, checkinputs.ErrInvalidCallGraph) {
				t.Fatalf("invalid batch accepted: %v", err)
			}
		})
	}
	if _, err := (documentRunner{documentRoute: shellcheckRoute}).Analyze(t.Context(), tools.Batch{}); err != nil {
		t.Fatalf("scopes-only batch failed: %v", err)
	}
}
