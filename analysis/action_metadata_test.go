package analysis_test

import (
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/knowledge"
)

func TestPublicAnalysisPreservesActionMetadataCoverage(t *testing.T) {
	result, err := analysis.CheckSources(t.Context(), analysis.Request{Options: analysis.Options{WorkingDir: t.TempDir()}, Sources: []analysis.Source{{Path: "ci.yml", Content: []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n")}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Reason != knowledge.LookupDisabledReason {
		t.Fatalf("coverage=%v", result.ActionMetadata)
	}
	content, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"action_metadata":[{"reference":"example/action@v1","state":"unavailable","reason":"lookup-disabled"`) {
		t.Fatalf("public JSON lost coverage: %s", content)
	}
}
