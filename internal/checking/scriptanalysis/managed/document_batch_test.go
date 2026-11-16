package managed

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
)

func TestDocumentBatchRejectsMissingFactsBeforeNegotiation(t *testing.T) {
	t.Parallel()
	valid := documentBatchFixture(t, 1)
	missingFact := valid
	missingFact.Scripts = append([]scriptanalysis.Unit(nil), valid.Scripts...)
	missingFact.Scripts[0].Reference = sourcetext.Reference{}
	foreign := valid
	foreign.Scripts = append([]scriptanalysis.Unit(nil), valid.Scripts...)
	foreign.Scripts[0].Occurrence = "unknown"
	for name, batch := range map[string]scriptanalysis.Batch{
		"missing graph": {Scripts: []scriptanalysis.Unit{{}}},
		"missing fact":  missingFact,
		"foreign fact":  foreign,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Nil callbacks prove invalid batches cannot reach negotiation or analysis.
			runner := documentRunner{documentRoute: shellcheckRoute, context: contract.Context{Document: checkinputs.AnalyzerInput{Reference: valid.CallGraph.Entry().Reference}}}
			_, err := runner.Analyze(t.Context(), batch)
			if !errors.Is(err, checkinputs.ErrInvalidCallGraph) {
				t.Fatalf("invalid batch accepted: %v", err)
			}
		})
	}
	if _, err := (documentRunner{documentRoute: shellcheckRoute}).Analyze(t.Context(), scriptanalysis.Batch{}); err != nil {
		t.Fatalf("scopes-only batch failed: %v", err)
	}
}
