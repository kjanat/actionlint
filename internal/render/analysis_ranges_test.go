package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/report"
)

func TestStructuredRenderRanges(t *testing.T) {
	for _, tc := range []struct {
		name       string
		end        diagmodel.Position
		github     string
		legacySpan string
	}{
		{"single line", diagmodel.Position{Line: 1, Column: 4}, "line=1,endLine=1,col=2,endColumn=3", "1:2-3"},
		{"multiple lines", diagmodel.Position{Line: 2, Column: 4}, "line=1,endLine=2", "1:2-5"},
		{"next line boundary", diagmodel.Position{Line: 2, Column: 1}, "line=1,endLine=1", "1:2-5"},
		{"later line boundary", diagmodel.Position{Line: 3, Column: 1}, "line=1,endLine=2", "1:2-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("abcde\nfghij\n")
			finding := diagmodel.Diagnostic{Path: "input.yml", Rule: "expression", Message: "bad range", Start: diagmodel.Position{Line: 1, Column: 2}, End: tc.end}
			result := report.Analysis{Diagnostics: []diagmodel.Diagnostic{finding}, Sources: []report.DiagnosticSource{{Content: source}}}

			legacy, err := New("", "{{range .}}{{.Line}}:{{.Column}}-{{.EndColumn}}{{end}}", false, testVersion, nil)
			if err != nil {
				t.Fatal(err)
			}
			var legacyOut bytes.Buffer
			if err := legacy.Render(&legacyOut, Prepare(result)); err != nil {
				t.Fatal(err)
			}
			if legacyOut.String() != tc.legacySpan {
				t.Fatalf("custom-template range changed: want %s, got %s", tc.legacySpan, legacyOut.String())
			}
			for _, format := range []Format{FormatSARIF, FormatGitHub} {
				t.Run(string(format), func(t *testing.T) {
					renderer, err := New(format, "", false, testVersion, nil)
					if err != nil {
						t.Fatal(err)
					}
					var out bytes.Buffer
					if err := renderer.Render(&out, Prepare(result)); err != nil {
						t.Fatal(err)
					}
					if format == FormatGitHub {
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

func TestUnavailablePointNeverInfersRereadTokenExtent(t *testing.T) {
	diagnostic := diagmodel.Diagnostic{Path: "reported.sh", Rule: "shellcheck", Message: "unavailable", Start: diagmodel.Position{Line: 1, Column: 6}, End: diagmodel.Position{Line: 1, Column: 6}}
	analysis := report.Analysis{Diagnostics: []diagmodel.Diagnostic{diagnostic}, Sources: []report.DiagnosticSource{{Content: []byte("echo long_unverified_token\n"), RawScript: true, Unavailable: true}}}
	result := Prepare(analysis)
	fields := result.Findings[0].TemplateFields()
	if fields.EndColumn != diagnostic.Start.Column || fields.Snippet != "echo long_unverified_token\n     ^" {
		t.Fatalf("template inferred an unverified token extent: %+v", fields)
	}
	for _, mode := range []struct {
		format   Format
		template string
	}{{format: FormatText}, {template: "{{range .}}{{.Snippet}}{{end}}"}} {
		renderer, err := New(mode.format, mode.template, false, testVersion, nil)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := renderer.Render(&output, result); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "~") || !strings.Contains(output.String(), "     ^") {
			t.Fatalf("unavailable point inferred text extent: %q", output.String())
		}
	}
	analysis.Diagnostics[0].End.Column = 10
	fields = Prepare(analysis).Findings[0].TemplateFields()
	if fields.EndColumn != 9 || !strings.HasSuffix(fields.Snippet, "     ^~~~") {
		t.Fatalf("reported extent was lost: %+v", fields)
	}
}

func TestExplicitTextFormatDisablesOneline(t *testing.T) {
	source := []byte("on: push\n")
	finding := diagmodel.Diagnostic{Path: "input.yml", Rule: "expression", Message: "bad text", Start: diagmodel.Position{Line: 1, Column: 1}}
	result := report.Analysis{Diagnostics: []diagmodel.Diagnostic{finding}, Sources: []report.DiagnosticSource{{Content: source}}}

	textRenderer, err := New(FormatText, "", true, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	var text bytes.Buffer
	if err := textRenderer.Render(&text, Prepare(result)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(text.Bytes(), []byte("1 | on: push")) {
		t.Fatalf("explicit text format omitted source snippet: %q", text.String())
	}

	onelineRenderer, err := New(FormatOneline, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	var oneline bytes.Buffer
	if err := onelineRenderer.Render(&oneline, Prepare(result)); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(oneline.Bytes(), []byte("1 | on: push")) {
		t.Fatalf("oneline format included source snippet: %q", oneline.String())
	}
}

func TestOnelineOmitsCapturedSnippet(t *testing.T) {
	finding := diagmodel.Diagnostic{
		Path: "input.yml", Rule: "expression", Message: "bad text",
		Start: diagmodel.Position{Line: 1, Column: 1}, Snippet: "on: push",
	}
	result := Prepare(report.Analysis{Diagnostics: []diagmodel.Diagnostic{finding}})
	for _, format := range []Format{"", FormatOneline} {
		t.Run(string(format), func(t *testing.T) {
			renderer, err := New(format, "", true, testVersion, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, source := range [][]byte{nil, []byte("on: push\n")} {
				result.Findings[0].Source = source
				var out bytes.Buffer
				if err := renderer.Render(&out, result); err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(out.Bytes(), []byte("on: push")) || bytes.Count(out.Bytes(), []byte("\n")) != 1 {
					t.Fatalf("one-line output included captured source: %q", out.String())
				}
			}
			if result.Findings[0].TemplateFields().Snippet == "" {
				t.Fatal("rendering discarded the original finding's snippet")
			}
		})
	}
	template, err := New("", "{{range .}}{{.Snippet}}{{end}}", true, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := template.Render(&out, result); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("on: push\n^")) {
		t.Fatalf("template lost captured snippet with one-line enabled: %q", out.String())
	}
}
