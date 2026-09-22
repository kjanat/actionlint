package actionlint

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNativeSARIFRetainsAnalyzerFixes(t *testing.T) {
	for _, tc := range []struct{ severity, level string }{
		{"error", "error"}, {"warning", "warning"}, {"info", "note"}, {"style", "note"}, {"", "error"},
	} {
		t.Run(tc.severity, func(t *testing.T) {
			diagnostic := Diagnostic{Rule: "shellcheck", Code: "SC2086", Severity: tc.severity, Message: "Quote", Path: "dir/my workflow.yml",
				Start: DiagnosticPosition{7, 12}, End: DiagnosticPosition{7, 16},
				Fixes: []DiagnosticFix{{Description: "Quote variable", Edits: []DiagnosticEdit{
					{Path: "dir/my workflow.yml", Start: DiagnosticPosition{7, 12}, End: DiagnosticPosition{7, 12}, Replacement: `"`},
					{Path: "dir/my workflow.yml", Start: DiagnosticPosition{7, 16}, End: DiagnosticPosition{7, 16}, Replacement: `"`},
				}}}}
			renderer, err := NewAnalysisRenderer(OutputFormatSARIF, "", false)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := renderer.Render(&out, &AnalysisResult{Diagnostics: []Diagnostic{diagnostic}}); err != nil {
				t.Fatal(err)
			}
			var document struct {
				Runs []struct {
					ColumnKind string
					Results    []struct {
						Level      string
						Properties struct{ ExternalCode string }
						Fixes      []sarifFix
						Locations  []struct {
							PhysicalLocation struct{ ArtifactLocation sarifArtifactLocation }
						}
					}
				}
			}
			if err := json.Unmarshal(out.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			run := document.Runs[0]
			result := run.Results[0]
			if run.ColumnKind != "unicodeCodePoints" || result.Level != tc.level || result.Properties.ExternalCode != diagnostic.Code {
				t.Fatalf("metadata lost: %s", out.String())
			}
			if result.Locations[0].PhysicalLocation.ArtifactLocation.URI != "dir/my%20workflow.yml" {
				t.Fatalf("invalid path URI: %s", out.String())
			}
			if len(result.Fixes) != 1 || len(result.Fixes[0].ArtifactChanges) != 1 {
				t.Fatalf("fix group split: %+v", result.Fixes)
			}
			change := result.Fixes[0].ArtifactChanges[0]
			if change.ArtifactLocation.URI != "dir/my%20workflow.yml" || change.ArtifactLocation.URIBaseID != "%SRCROOT%" || len(change.Replacements) != 2 {
				t.Fatalf("fix path or group lost: %+v", change)
			}
			for i, replacement := range change.Replacements {
				edit := diagnostic.Fixes[0].Edits[i]
				if replacement.DeletedRegion != (sarifRegion{edit.Start.Line, edit.Start.Column, edit.End.Line, edit.End.Column}) || replacement.InsertedContent.Text != edit.Replacement {
					t.Fatalf("edit changed: %+v", replacement)
				}
			}
		})
	}
}
