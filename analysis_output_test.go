package actionlint

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestAnalysisSourceRanges(t *testing.T) {
	rule := NewRuleBase("test", "")
	rule.errorfRange(&Pos{Line: 1, Col: 2}, &Pos{Line: 2, Col: 4}, "multi-line")
	diagnostic := rule.Errs()[0].diagnostic([]byte("abc\ndefgh"))
	if diagnostic.Start != (DiagnosticPosition{1, 2}) || diagnostic.End != (DiagnosticPosition{2, 4}) {
		t.Fatal(diagnostic)
	}
	if rule.Errs()[0].GetTemplateFields([]byte("abc\ndefgh")).EndColumn != 3 {
		t.Fatal("legacy range changed")
	}
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "input.yml", Content: []byte(commandBadWorkflow)}}})
	if err != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestEffectiveConfigFieldCoverage(t *testing.T) {
	resolved := effectiveConfig(&Config{})
	var check func(reflect.Type, map[string]any)
	check = func(typ reflect.Type, values map[string]any) {
		t.Helper()
		keys := map[string]bool{}
		for field := range typ.Fields() {
			if !field.IsExported() {
				continue
			}
			key, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if key == "-" {
				continue
			}
			keys[key] = true
			value, ok := values[key]
			if !ok {
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
	fields := []*ErrorTemplateFields{{Filepath: "a%,:\r\nb.yml", Message: "bad%\r\n::notice::text", Kind: "expression", Line: 2, Column: 3, EndColumn: 4}}
	if err := (githubDiagnosticFormatter{}).Print(&out, fields); err != nil {
		t.Fatal(err)
	}
	want := "::error file=a%25%2C%3A%0D%0Ab.yml,line=2,col=3,endColumn=4,title=expression::bad%25%0D%0A::notice::text\n"
	if out.String() != want {
		t.Fatalf("annotation escaping: %q", out.String())
	}
}
