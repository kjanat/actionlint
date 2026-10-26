package shellcheck

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools"
)

func TestDocumentBatchRejectsMissingFactsBeforeNegotiation(t *testing.T) {
	for name, batch := range map[string]tools.Batch{
		"missing graph": {Scripts: []tools.Script{{}}},
		"missing fact":  {CallGraph: &analyzer.CallGraph{}, Scripts: []tools.Script{{}}},
		"foreign fact":  {CallGraph: &analyzer.CallGraph{}, Scripts: []tools.Script{{Fact: analyzer.Script{Occurrence: analyzer.ScriptOccurrence{ID: "unknown"}}}}},
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
