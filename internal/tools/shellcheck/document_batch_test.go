package shellcheck

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools"
)

func TestDocumentBatchRejectsMissingFactsBeforeNegotiation(t *testing.T) {
	valid := documentBatchFixture(t, 1)
	missingFact := valid
	missingFact.Scripts = append([]tools.Script(nil), valid.Scripts...)
	missingFact.Scripts[0].Fact = analyzer.Script{}
	foreign := valid
	foreign.Scripts = append([]tools.Script(nil), valid.Scripts...)
	foreign.Scripts[0].Fact.Occurrence.ID = "unknown"
	for name, batch := range map[string]tools.Batch{
		"missing graph": {Scripts: []tools.Script{{}}},
		"missing fact":  missingFact,
		"foreign fact":  foreign,
	} {
		t.Run(name, func(t *testing.T) {
			// Nil callbacks prove invalid batches cannot reach negotiation or analysis.
			runner := documentPluginRunner{context: tools.Context{Document: analyzer.Input{Reference: valid.CallGraph.Entry().Reference}}}
			_, err := runner.Analyze(t.Context(), batch)
			if !errors.Is(err, analyzer.ErrInvalidCallGraph) {
				t.Fatalf("invalid batch accepted: %v", err)
			}
		})
	}
	if _, err := (documentPluginRunner{}).Analyze(t.Context(), tools.Batch{}); err != nil {
		t.Fatalf("scopes-only batch failed: %v", err)
	}
}
