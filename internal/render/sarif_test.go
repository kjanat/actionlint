package render

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/rule"
)

func TestNativeSARIFUNCPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC filesystem paths require Windows")
	}
	for _, path := range []string{`\\server\share\my workflow.yml`, `//server/share/my workflow.yml`} {
		t.Run(path, func(t *testing.T) {
			finding := diagnostic.Diagnostic{
				Rule: "shellcheck", Message: "Quote", Path: path,
				Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 2},
				Fixes: []diagnostic.Fix{{Description: "Quote", Edits: []diagnostic.Edit{
					{Path: path, Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 2}, Replacement: `"$x"`},
				}}},
			}
			renderer, err := New(FormatSARIF, "", false, testVersion, nil)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := renderer.Render(&out, Result{Diagnostics: []diagnostic.Diagnostic{finding}}); err != nil {
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
			if got := result.Locations[0].PhysicalLocation.ArtifactLocation; got.URI != want || got.URIBaseID != "" {
				t.Errorf("diagnostic artifact = %+v, want URI %q without base", got, want)
			}
			if got := result.Fixes[0].ArtifactChanges[0].ArtifactLocation; got.URI != want || got.URIBaseID != "" {
				t.Errorf("fix artifact = %+v, want URI %q without base", got, want)
			}
		})
	}
}

func TestNativeSARIFAbsoluteArtifactBase(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "workflow.yml")
	{
		renderer, err := New(FormatSARIF, "", false, testVersion, nil)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err = renderer.Render(&out, Result{Diagnostics: []diagnostic.Diagnostic{{Path: absolute, Rule: "test", Message: "test", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 2}}}})
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Runs []struct {
				Results []struct {
					Locations []struct {
						PhysicalLocation struct{ ArtifactLocation sarifArtifactLocation }
					}
				}
			}
		}
		if err := json.Unmarshal(out.Bytes(), &document); err != nil {
			t.Fatal(err)
		}
		artifact := document.Runs[0].Results[0].Locations[0].PhysicalLocation.ArtifactLocation
		if artifact.URIBaseID != "" {
			t.Errorf("absolute artifact base = %q, want none", artifact.URIBaseID)
		}
	}
}

func TestNativeSARIFRetainsAnalyzerFixes(t *testing.T) {
	for _, tc := range []struct{ severity, level string }{
		{"error", "error"}, {"warning", "warning"}, {"info", "note"}, {"", "error"},
	} {
		t.Run(tc.severity, func(t *testing.T) {
			finding := diagnostic.Diagnostic{
				Rule: "shellcheck", Code: "SC2086", Severity: tc.severity, Message: "Quote", Path: "dir/my workflow.yml",
				Start: diagnostic.Position{Line: 7, Column: 12}, End: diagnostic.Position{Line: 7, Column: 16},
				Fixes: []diagnostic.Fix{{Description: "Quote variable", Edits: []diagnostic.Edit{
					{Path: "dir/my workflow.yml", Start: diagnostic.Position{Line: 7, Column: 12}, End: diagnostic.Position{Line: 7, Column: 12}, Replacement: `"`},
					{Path: "dir/my workflow.yml", Start: diagnostic.Position{Line: 7, Column: 16}, End: diagnostic.Position{Line: 7, Column: 16}, Replacement: `"`},
				}}},
			}
			renderer, err := New(FormatSARIF, "", false, testVersion, nil)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := renderer.Render(&out, Result{Diagnostics: []diagnostic.Diagnostic{finding}}); err != nil {
				t.Fatal(err)
			}
			var document struct {
				Runs []struct {
					ColumnKind string
					Results    []struct {
						Level      string
						Properties struct{ DiagnosticCode string }
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
			if run.ColumnKind != "unicodeCodePoints" || result.Level != tc.level || result.Properties.DiagnosticCode != finding.Code {
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
				edit := finding.Fixes[0].Edits[i]
				if replacement.DeletedRegion != (sarifRegion{StartLine: edit.Start.Line, StartColumn: edit.Start.Column, EndLine: edit.End.Line, EndColumn: edit.End.Column}) || replacement.InsertedContent.Text != edit.Replacement {
					t.Fatalf("edit changed: %+v", replacement)
				}
			}
		})
	}
}

func testVersion() string { return "test" }

func TestSARIFSharedRuleMetadataAndRelatedLocations(t *testing.T) {
	metadata := Rule{Name: "custom-rule", Description: "summary", Explanation: "explanation", Help: "repair instructions", DocumentationURL: "https://example.com/custom-rule", Examples: []rule.Example{{Description: "original example"}}}
	renderer, err := New(FormatSARIF, "", false, testVersion, []Rule{metadata})
	if err != nil {
		t.Fatal(err)
	}
	metadata.Examples[0].Description = "mutated example"
	if renderer.rules.sorted()[0].Examples[0].Description != "original example" {
		t.Fatal("renderer changed caller-owned metadata")
	}
	var output bytes.Buffer
	related := diagnostic.Related{Message: "previous declaration", Path: "other workflow.yml", Start: diagnostic.Position{Line: 2, Column: 3}, End: diagnostic.Position{Line: 2, Column: 7}}
	err = renderer.Render(&output, Result{Diagnostics: []diagnostic.Diagnostic{{Rule: metadata.Name, Message: "duplicate", Path: "workflow.yml", Start: diagnostic.Position{Line: 3, Column: 1}, End: diagnostic.Position{Line: 3, Column: 4}, Related: []diagnostic.Related{related}}}})
	if err != nil {
		t.Fatal(err)
	}
	var document sarifDocument
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	info := document.Runs[0].Tool.Driver.Rules[0]
	if info.HelpURI != metadata.DocumentationURL || info.FullDescription.Text != metadata.Explanation || info.Help == nil || info.Help.Text != metadata.Help {
		t.Fatalf("rule metadata lost: %+v", info)
	}
	locations := document.Runs[0].Results[0].RelatedLocations
	if len(locations) != 1 || locations[0].Message.Text != related.Message || locations[0].PhysicalLocation.ArtifactLocation.URI != "other%20workflow.yml" || locations[0].PhysicalLocation.Region.StartColumn != 3 {
		t.Fatalf("related location lost: %+v", locations)
	}
}
