package managed

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
)

func TestDocumentBatchRejectsMissingFactsBeforeNegotiation(t *testing.T) {
	t.Parallel()
	valid := documentBatchFixture(t, 1)
	missingFact := valid
	missingFact.Scripts = append([]scripts.Unit(nil), valid.Scripts...)
	missingFact.Scripts[0].Reference = source.Reference{}
	foreign := valid
	foreign.Scripts = append([]scripts.Unit(nil), valid.Scripts...)
	foreign.Scripts[0].Occurrence = "unknown"
	for name, batch := range map[string]scripts.Batch{
		"missing graph": {Scripts: []scripts.Unit{{}}},
		"missing fact":  missingFact,
		"foreign fact":  foreign,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Nil callbacks prove invalid batches cannot reach negotiation or analysis.
			runner := documentRunner{documentRoute: shellcheckRoute, context: contract.Context{Document: inputs.AnalyzerInput{Reference: valid.CallGraph.Entry().Reference}}}
			_, err := runner.Analyze(t.Context(), batch)
			if !errors.Is(err, inputs.ErrInvalidCallGraph) {
				t.Fatalf("invalid batch accepted: %v", err)
			}
		})
	}
	if _, err := (documentRunner{documentRoute: shellcheckRoute}).Analyze(t.Context(), scripts.Batch{}); err != nil {
		t.Fatalf("scopes-only batch failed: %v", err)
	}
}
