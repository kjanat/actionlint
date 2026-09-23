package actionlint

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"
)

func TestNativeSARIFUNCPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC filesystem paths require Windows")
	}
	for _, path := range []string{`\\server\share\my workflow.yml`, `//server/share/my workflow.yml`} {
		t.Run(path, func(t *testing.T) {
			diagnostic := Diagnostic{Rule: "shellcheck", Message: "Quote", Path: path,
				Start: DiagnosticPosition{1, 1}, End: DiagnosticPosition{1, 2},
				Fixes: []DiagnosticFix{{Description: "Quote", Edits: []DiagnosticEdit{
					{Path: path, Start: DiagnosticPosition{1, 1}, End: DiagnosticPosition{1, 2}, Replacement: `"$x"`},
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
					Results []struct {
						Fixes     []sarifFix
						Locations []struct {
							PhysicalLocation struct{ ArtifactLocation sarifArtifactLocation }
						}
					}
				}
			}
			if err := json.Unmarshal(out.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			result := document.Runs[0].Results[0]
			const want = "file://server/share/my%20workflow.yml"
			if got := result.Locations[0].PhysicalLocation.ArtifactLocation.URI; got != want {
				t.Errorf("diagnostic URI = %q, want %q", got, want)
			}
			if got := result.Fixes[0].ArtifactChanges[0].ArtifactLocation; got.URI != want || got.URIBaseID != "" {
				t.Errorf("fix artifact = %+v, want URI %q without base", got, want)
			}
		})
	}
}

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
