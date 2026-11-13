package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestJSONRendererUsesSuppliedCompletion(t *testing.T) {
	t.Parallel()
	for _, diagnostics := range [][]results.Diagnostic{nil, {{Rule: "test", Message: "partial finding", Path: "workflow.yml"}}} {
		for _, format := range []Format{FormatJSON, FormatJSONL} {
			renderer, err := New(format, "", false, func() string { return "test" }, nil)
			if err != nil {
				t.Fatal(err)
			}
			given := report.CheckResult{SchemaVersion: 1, Status: "failure", Completed: false, ExitCode: 3, Error: "dependency unavailable", Diagnostics: diagnostics}
			var output bytes.Buffer
			if err := renderer.Render(&output, Result{CheckResult: given}); err != nil {
				t.Fatal(err)
			}
			if format == FormatJSON {
				var actual report.CheckResult
				if err := json.Unmarshal(output.Bytes(), &actual); err != nil {
					t.Fatal(err)
				}
				if actual.Status != given.Status || actual.Completed != given.Completed || actual.ExitCode != given.ExitCode || actual.Error != given.Error {
					t.Fatalf("renderer changed command completion: %+v", actual)
				}
			} else if strings.Count(output.String(), "\n") != len(diagnostics) {
				t.Fatalf("JSONL should contain only findings: %q", output.String())
			}
		}
	}
}

func TestJSONRendererRequiresSuppliedCompletion(t *testing.T) {
	t.Parallel()
	for _, format := range []Format{FormatJSON, FormatJSONL} {
		renderer, err := New(format, "", false, func() string { return "test" }, nil)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := renderer.Render(&output, FromAnalysis(report.Analysis{})); err == nil || !strings.Contains(err.Error(), "explicit status") {
			t.Fatalf("missing command status was not rejected: %v", err)
		}
		if output.Len() != 0 {
			t.Fatal("renderer wrote an incomplete JSON envelope")
		}
	}
}
