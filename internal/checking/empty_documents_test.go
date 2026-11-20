package checking

import (
	"encoding/json"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestAnalysisEmptyDocuments(t *testing.T) {
	t.Parallel()
	result, err := runTestAnalysis(t.Context(), analysisRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []report.CheckResult{exitcode.AnalysisResult(result.Analysis, false), exitcode.AnalysisResult((&analysisResult{}).Analysis, false), exitcode.NewCheckResult(3)} {
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		if string(object["documents"]) != "[]" {
			t.Fatalf("empty inventory omitted or null: %s", data)
		}
	}
}
