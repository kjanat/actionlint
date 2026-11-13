package checking_test

import (
	"bytes"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/output"
)

func TestCheckJSONPreservesActionMetadataCoverage(t *testing.T) {
	t.Parallel()
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Availability.Reason() != availability.LookupDisabledReason {
		t.Fatalf("coverage=%v", result.ActionMetadata)
	}
	var content bytes.Buffer
	if err := output.WriteJSON(&content, exitcode.AnalysisResult(*result), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content.String(), `"action_metadata":[{"reference":"example/action@v1","state":"unavailable","reason":"lookup-disabled"`) {
		t.Fatalf("check JSON lost coverage: %s", &content)
	}
}
