package githubaction

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func analysisForRender(t *testing.T, prefix string) *actionlint.AnalysisResult {
	t.Helper()
	source := prefix + "on: push\njobs:\n  test:\n    runs-on: \"a<b>&c\"\n    steps: [{run: 'echo ok'}]\n"
	return analysisForRenderSource(t, source)
}

func analysisForRenderSource(t *testing.T, source string) *actionlint.AnalysisResult {
	t.Helper()
	result, err := actionlint.Analyze(context.Background(), actionlint.AnalysisRequest{
		WorkingDir: t.TempDir(),
		Sources:    []actionlint.SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("wanted one finding: %+v", result.Diagnostics)
	}
	result.Diagnostics[0].Message = "label \"a<b>&c\" is unknown"
	return result
}

func TestRenderAnalysisPreservesUnicodeAndLegacyRanges(t *testing.T) {
	for _, tc := range []struct {
		name              string
		end               actionlint.DiagnosticPosition
		column, indicator string
	}{
		{"single line", actionlint.DiagnosticPosition{Line: 4, Column: 17}, "16", "^~~~"},
		{"multiple lines", actionlint.DiagnosticPosition{Line: 5, Column: 2}, "17", "^~~~~"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			analysis := analysisForRenderSource(t, "on: push\njobs:\n  test:\n    runs-on: \"é🐚\"\n    steps: [{run: 'echo ok'}]\n")
			analysis.Diagnostics[0].End = tc.end
			for _, format := range []outputFormat{formatDefault, formatGitHub, formatMarkdown} {
				got, err := renderAnalysis(format, analysis, ".", ".")
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(got, "runs-on: \"é🐚\"") || !strings.Contains(got, tc.indicator) || strings.Contains(got, tc.indicator+"~") {
					t.Fatalf("Unicode source or caret width changed: %q", got)
				}
				if format == formatGitHub && !strings.Contains(got, "endColumn="+tc.column+",") {
					t.Fatalf("legacy inclusive column changed: %q", got)
				}
			}
		})
	}
}

func TestCommandEscape(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""}, {"plain", "plain"}, {"100%", "100%25"},
		{"a\nb", "a%0Ab"}, {"a\r\nb", "a%0D%0Ab"}, {"a:b,c", "a:b,c"}, {"%0A", "%250A"},
	} {
		if got := commandEscape(tc.in); got != tc.want {
			t.Errorf("commandEscape(%q) = %q, wanted %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderAnalysisCompatibility(t *testing.T) {
	analysis := analysisForRender(t, "")
	for _, tc := range []struct {
		format outputFormat
		want   string
	}{
		{formatDefault, "workflow.yaml:4:14: label \"a<b>&c\" is unknown [runner-label]\n  |\n4 |     runs-on: \"a<b>&c\"\n  |              ^~~~~~~~\n"},
		{formatOneline, "workflow.yaml:4:14: label \"a<b>&c\" is unknown [runner-label]\n"},
		{formatGitHub, "::error file=workflow.yaml,line=4,col=14,endColumn=21,title=actionlint (runner-label)::label \"a<b>&c\" is unknown%0A%0A    runs-on: \"a<b>&c\"%0A             ^~~~~~~~\n"},
		{formatMarkdown, "### workflow.yaml:4:14 (runner-label)\n\nlabel \"a<b>&c\" is unknown\n\n        runs-on: \"a<b>&c\"\n                 ^~~~~~~~\n\n"},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			got, err := renderAnalysis(tc.format, analysis, ".", ".")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("wanted\n%q\nbut got\n%q", tc.want, got)
			}
		})
	}
}

func TestRenderAnalysisEscapesCommandsAndRebasesPaths(t *testing.T) {
	analysis := analysisForRender(t, "")
	finding := &analysis.Diagnostics[0]
	finding.Path = "work,flow:1.yaml"
	finding.Message = "100%\r\n::warning::spoof"
	got, err := renderAnalysis(formatGitHub, analysis, "sub", ".")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "::error file=sub/work%2Cflow%3A1.yaml,") || !strings.Contains(got, "::100%25%0D%0A::warning::spoof%0A%0A") {
		t.Fatalf("command escaping or path rebasing lost: %q", got)
	}
	if finding.Path != "work,flow:1.yaml" {
		t.Fatal("annotation rendering mutated canonical path")
	}
	finding.Path = "::odd.yaml"
	for _, format := range []outputFormat{formatDefault, formatOneline} {
		got, err := renderAnalysis(format, analysis, ".", ".")
		if err != nil || !strings.HasPrefix(got, "./::odd.yaml:4:14:") {
			t.Fatalf("command-like filename must be escaped: %q, %v", got, err)
		}
	}
}

func TestRenderAnalysisWideLinesAndAbsentSource(t *testing.T) {
	analysis := analysisForRender(t, strings.Repeat("\n", 1230))
	got, err := renderAnalysis(formatDefault, analysis, ".", ".")
	if err != nil || !strings.Contains(got, "\n     |\n1234 |     runs-on:") {
		t.Fatalf("wide line number gutter: %q, %v", got, err)
	}
	analysis.Diagnostics[0].Start.Line = 2000
	analysis.Diagnostics[0].End.Line = 2000
	for _, format := range []outputFormat{formatDefault, formatOneline, formatGitHub, formatMarkdown} {
		got, err := renderAnalysis(format, analysis, ".", ".")
		if err != nil || strings.Contains(got, "runs-on:") || strings.Contains(got, "^") {
			t.Fatalf("missing source should omit snippet: %q, %v", got, err)
		}
	}
}

func TestRenderAnalysisEmptyAndUnknownFormat(t *testing.T) {
	analysis := &actionlint.AnalysisResult{}
	for _, format := range []outputFormat{formatDefault, formatOneline, formatGitHub, formatMarkdown} {
		got, err := renderAnalysis(format, analysis, ".", ".")
		if err != nil || got != "" {
			t.Fatalf("empty analysis: %q, %v", got, err)
		}
	}
	if _, err := renderAnalysis(formatSARIF, analysis, ".", "."); err == nil {
		t.Fatal("wanted error for unsupported compatibility format")
	}
}

func TestRenderOutcomeUsesTypedDiagnostics(t *testing.T) {
	analysis := analysisForRender(t, "")
	for _, serialized := range []string{"workflow.yaml:4:14: finding\n", "{\"runs\":[{\"results\":[{}]}]}\n"} {
		result := &lintResult{lintOutcome: &lintOutcome{serialized, "", actionlint.ExitStatusSuccessProblemFound}, diagnostics: analysis.Diagnostics}
		o, count, rendered := renderOutcome(result)
		if o.code != actionlint.ExitStatusSuccessProblemFound || count != "1" || rendered != serialized {
			t.Fatalf("typed finding count/rendered output changed: %+v, %q, %q", o, count, rendered)
		}
	}
}

func TestRenderOutcomeKeepsFailureOutput(t *testing.T) {
	result := &lintResult{lintOutcome: &lintOutcome{"partial", "boom\n", actionlint.ExitStatusFailure}}
	o, count, rendered := renderOutcome(result)
	if o.code != actionlint.ExitStatusFailure || count != "" || rendered != "boom\npartial" {
		t.Fatalf("failure output changed: %+v, %q, %q", o, count, rendered)
	}
	result.stderr = ""
	_, _, rendered = renderOutcome(result)
	if rendered != "partial" {
		t.Errorf("wanted partial output, got %q", rendered)
	}
}
