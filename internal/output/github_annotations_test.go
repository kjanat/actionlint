package output_test

import (
	"bytes"
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	render "actionlint.kjanat.dev/v2/internal/output"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
)

func TestGitHubAnnotationEscaping(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	diagnostics := []diagmodel.Diagnostic{{Path: "a%,:\r\nb.yml", Message: "bad%\r\n::notice::text", Rule: "expression", Start: sourcepos.Position{Line: 2, Column: 3}, End: sourcepos.Position{Line: 2, Column: 5}}}
	if err := render.WriteGitHub(&out, diagnostics); err != nil {
		t.Fatal(err)
	}
	want := "::error file=a%25%2C%3A%0D%0Ab.yml,line=2,endLine=2,col=3,endColumn=4,title=expression::bad%25%0D%0A::notice::text\n"
	if out.String() != want {
		t.Fatalf("annotation escaping: %q", out.String())
	}
}

func TestGitHubAnnotationLevels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ severity, annotation string }{{"", "error"}, {"error", "error"}, {"warning", "warning"}, {"info", "notice"}} {
		var out bytes.Buffer
		if err := render.WriteGitHub(&out, []diagmodel.Diagnostic{{Severity: tc.severity, Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 2}}}); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), "::"+tc.annotation+" ") {
			t.Fatal(out.String())
		}
	}
}

func TestGitHubAnnotationUsesDiagnosticCode(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"missing-file", "runtime-deprecated", "SC2035"} {
		var out bytes.Buffer
		if err := render.WriteGitHub(&out, []diagmodel.Diagnostic{{Rule: "action", Code: code, Message: "problem"}}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), ",title="+code+"::problem\n") {
			t.Fatalf("lost specific code in annotation: %s", &out)
		}
	}
}

func TestTemplateRangeUsesInclusiveEndColumn(t *testing.T) {
	t.Parallel()
	d := diagmodel.Diagnostic{Start: sourcepos.Position{Line: 1, Column: 2}, End: sourcepos.Position{Line: 2, Column: 4}}
	if got := render.FindingOf(d, []byte("abc\ndefgh")).TemplateFields().EndColumn; got != 3 {
		t.Fatalf("want inclusive column 3, got %d", got)
	}
}
