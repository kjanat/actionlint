package application

import (
	"encoding/json"
	"testing"

	"actionlint.kjanat.dev/internal/render"
)

func TestAnalysisEmptyDocuments(t *testing.T) {
	result, err := runTestAnalysis(t.Context(), analysisRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []render.CheckResult{render.Prepare(result.Analysis).CheckResult(), render.Prepare((&analysisResult{}).Analysis).CheckResult(), render.NewCheckResult(3)} {
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
