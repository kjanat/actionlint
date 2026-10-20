package render_test

import (
	"bytes"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/render"
)

func TestGitHubAnnotationEscaping(t *testing.T) {
	var out bytes.Buffer
	diagnostics := []diagmodel.Diagnostic{{Path: "a%,:\r\nb.yml", Message: "bad%\r\n::notice::text", Rule: "expression", Start: diagmodel.Position{Line: 2, Column: 3}, End: diagmodel.Position{Line: 2, Column: 5}}}
	if err := render.WriteGitHub(&out, diagnostics); err != nil {
		t.Fatal(err)
	}
	want := "::error file=a%25%2C%3A%0D%0Ab.yml,line=2,endLine=2,col=3,endColumn=4,title=expression::bad%25%0D%0A::notice::text\n"
	if out.String() != want {
		t.Fatalf("annotation escaping: %q", out.String())
	}
}

func TestGitHubAnnotationLevels(t *testing.T) {
	for _, tc := range []struct{ severity, annotation string }{{"", "error"}, {"error", "error"}, {"warning", "warning"}, {"info", "notice"}, {"style", "notice"}} {
		var out bytes.Buffer
		if err := render.WriteGitHub(&out, []diagmodel.Diagnostic{{Severity: tc.severity, Start: diagmodel.Position{Line: 1, Column: 1}, End: diagmodel.Position{Line: 1, Column: 2}}}); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), "::"+tc.annotation+" ") {
			t.Fatal(out.String())
		}
	}
}

func TestTemplateRangeUsesInclusiveEndColumn(t *testing.T) {
	d := diagmodel.Diagnostic{Start: diagmodel.Position{Line: 1, Column: 2}, End: diagmodel.Position{Line: 2, Column: 4}}
	if got := render.FindingOf(d, []byte("abc\ndefgh")).TemplateFields().EndColumn; got != 3 {
		t.Fatalf("want inclusive column 3, got %d", got)
	}
}
