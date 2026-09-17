package actionlint

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestStructuredRenderRanges(t *testing.T) {
	for _, tc := range []struct {
		name       string
		end        DiagnosticPosition
		github     string
		legacySpan string
	}{
		{"single line", DiagnosticPosition{1, 4}, "line=1,endLine=1,col=2,endColumn=3", "1:2-3"},
		{"multiple lines", DiagnosticPosition{2, 4}, "line=1,endLine=2", "1:2-5"},
		{"next line boundary", DiagnosticPosition{2, 1}, "line=1,endLine=1", "1:2-5"},
		{"later line boundary", DiagnosticPosition{3, 1}, "line=1,endLine=2", "1:2-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("abcde\nfghij\n")
			finding := &Error{Filepath: "input.yml", Kind: "expression", Message: "bad range", Line: 1, Column: 2,
				endPosition: &Pos{Line: tc.end.Line, Col: tc.end.Column}}
			result := &AnalysisResult{Diagnostics: []Diagnostic{finding.diagnostic(source)}, files: []analyzedFile{{
				source: SourceUnit{Path: "input.yml", Content: source}, errors: []*Error{finding},
			}}}
			legacy, err := NewAnalysisRenderer("", "{{range .}}{{.Line}}:{{.Column}}-{{.EndColumn}}{{end}}", false)
			if err != nil {
				t.Fatal(err)
			}
			var legacyOut bytes.Buffer
			if err := legacy.Render(&legacyOut, result); err != nil {
				t.Fatal(err)
			}
			if legacyOut.String() != tc.legacySpan {
				t.Fatalf("custom-template range changed: want %s, got %s", tc.legacySpan, legacyOut.String())
			}
			for _, format := range []OutputFormat{OutputFormatSARIF, OutputFormatGitHub} {
				t.Run(string(format), func(t *testing.T) {
					renderer, err := NewAnalysisRenderer(format, "", false)
					if err != nil {
						t.Fatal(err)
					}
					var out bytes.Buffer
					if err := renderer.Render(&out, result); err != nil {
						t.Fatal(err)
					}
					if format == OutputFormatGitHub {
						want := "::error file=input.yml," + tc.github + ",title=expression::bad range\n"
						if out.String() != want {
							t.Fatalf("want %s, got %s", want, out.String())
						}
						return
					}
					var document struct {
						Runs []struct {
							Results []struct {
								Locations []struct {
									PhysicalLocation struct {
										Region struct{ StartLine, StartColumn, EndLine, EndColumn int }
									}
								}
							}
						}
					}
					if err := json.Unmarshal(out.Bytes(), &document); err != nil {
						t.Fatal(err)
					}
					region := document.Runs[0].Results[0].Locations[0].PhysicalLocation.Region
					if region.StartLine != 1 || region.StartColumn != 2 || region.EndLine != tc.end.Line || region.EndColumn != tc.end.Column {
						t.Fatalf("range lost: %+v, want end %+v", region, tc.end)
					}
				})
			}
		})
	}
}
