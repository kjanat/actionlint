package engine

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/render"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

func TestAnalysisSourceRanges(t *testing.T) {
	rule := workflowvisit.NewBase("test", "")
	rule.ErrorfRange(&workflowlang.Pos{Line: 1, Col: 2}, &workflowlang.Pos{Line: 2, Col: 4}, "multi-line")
	diagnostic := rule.Errs()[0].Canonical([]byte("abc\ndefgh"))
	if diagnostic.Start != (diagmodel.Position{Line: 1, Column: 2}) || diagnostic.End != (diagmodel.Position{Line: 2, Column: 4}) {
		t.Fatal(diagnostic)
	}
	if render.FindingOf(rule.Errs()[0].Diagnostic, []byte("abc\ndefgh")).TemplateFields().EndColumn != 3 {
		t.Fatal("legacy range changed")
	}
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "input.yml", Content: []byte(commandBadWorkflow)}}})
	if err != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestEffectiveConfigFieldCoverage(t *testing.T) {
	resolved, err := repoconfig.EffectiveValues(&Config{})
	if err != nil {
		t.Fatal(err)
	}
	var check func(reflect.Type, map[string]any)
	check = func(typ reflect.Type, values map[string]any) {
		t.Helper()
		keys := map[string]bool{}
		for field := range typ.Fields() {
			if !field.IsExported() {
				continue
			}
			key, options, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if key == "-" {
				continue
			}
			keys[key] = true
			value, ok := values[key]
			if !ok {
				// Omit optional unset selections to preserve inheritance.
				if strings.Contains(options, "omitempty") {
					continue
				}
				t.Errorf("%s.%s missing from resolved configuration", typ, field.Name)
				continue
			}
			if nested, ok := value.(map[string]any); ok && field.Type.Kind() == reflect.Struct {
				check(field.Type, nested)
			}
		}
		for key := range values {
			if !keys[key] {
				t.Errorf("resolved key %s not in %s", key, typ)
			}
		}
	}
	check(reflect.TypeFor[Config](), resolved)
}

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
