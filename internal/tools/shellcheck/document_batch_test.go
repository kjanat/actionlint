package shellcheck

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools"
)

func TestDocumentBatchRejectsMissingFactsBeforeNegotiation(t *testing.T) {
	valid := documentBatchFixture(t, 1)
	foreign := valid
	foreign.Scripts = append([]tools.Script(nil), valid.Scripts...)
	foreign.Scripts[0].Fact.Occurrence.ID = "unknown"
	for name, batch := range map[string]tools.Batch{
		"missing graph": {Scripts: []tools.Script{{}}},
		"missing fact":  {CallGraph: valid.CallGraph, Scripts: valid.Scripts},
		"foreign fact":  foreign,
	} {
		t.Run(name, func(t *testing.T) {
			// Nil callbacks prove invalid batches cannot reach negotiation or analysis.
			_, err := (documentPluginRunner{}).Analyze(t.Context(), batch)
			if !errors.Is(err, analyzer.ErrInvalidCallGraph) {
				t.Fatalf("invalid batch accepted: %v", err)
			}
		})
	}
	if _, err := (documentPluginRunner{}).Analyze(t.Context(), tools.Batch{}); err != nil {
		t.Fatalf("scopes-only batch failed: %v", err)
	}
}
