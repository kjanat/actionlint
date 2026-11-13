package output

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestFormatterGitHubCommandEscaping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input, data, property string
	}{
		{"empty", "", "", ""},
		{"plain", "build ünicode/path.yml", "build ünicode/path.yml", "build ünicode/path.yml"},
		{"percent", "%0A%25", "%250A%2525", "%250A%2525"},
		{"line endings", "a\rb\nc\r\nd", "a%0Db%0Ac%0D%0Ad", "a%0Db%0Ac%0D%0Ad"},
		{"separators", "C:/build,job=x::text", "C:/build,job=x::text", "C%3A/build%2Cjob=x%3A%3Atext"},
		{"command injection", "%\r\n::error file=x,y::message", "%25%0D%0A::error file=x,y::message", "%25%0D%0A%3A%3Aerror file=x%2Cy%3A%3Amessage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, escape := range []struct{ function, want string }{{"escapeData", tc.data}, {"escapeProperty", tc.property}} {
				for _, format := range []string{"{{range .}}{{" + escape.function + " .Message}}{{end}}", "{{range .}}{{.Message | " + escape.function + "}}{{end}}"} {
					formatter, err := NewFormatter(format, func() string { return "" }, nil)
					if err != nil {
						t.Fatal(err)
					}
					var out strings.Builder
					if err := formatter.Print(&out, []*TemplateFields{{Message: tc.input}}); err != nil {
						t.Fatal(err)
					}
					if got := out.String(); got != escape.want {
						t.Errorf("%s(%q) = %q, want %q", format, tc.input, got, escape.want)
					}
				}
			}
		})
	}
}

func TestFormatterGitHubAnnotationExample(t *testing.T) {
	t.Parallel()
	format := "{{range $err := .}}::error file={{escapeProperty $err.Filepath}},line={{$err.Line}},col={{$err.Column}}::{{escapeData $err.Message}}%0A```%0A{{escapeData $err.Snippet}}%0A```\n{{end}}"
	formatter, err := NewFormatter(format, func() string { return "" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := formatter.Print(&out, []*TemplateFields{{
		Filepath: "C:/build,job=x/ci.yml", Line: 2, Column: 3,
		Message: "bad%0A\r\n::notice::text", Snippet: "run: echo 100%\r\n  ^",
	}}); err != nil {
		t.Fatal(err)
	}
	want := "::error file=C%3A/build%2Cjob=x/ci.yml,line=2,col=3::bad%250A%0D%0A::notice::text%0A```%0Arun: echo 100%25%0D%0A  ^%0A```\n"
	if got := out.String(); got != want {
		t.Fatalf("annotation = %q, want %q", got, want)
	}
}

func TestFormatterPrintJSONEncodeError(t *testing.T) {
	t.Parallel()
	f, err := NewFormatter("{{json .}}", func() string { return "" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	err = f.temp.Execute(&b, math.NaN())
	if err == nil {
		t.Fatal("error did not occur", b.String())
	}
	want := "could not encode template value into JSON"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%q is not contained in error message %q", want, err.Error())
	}
}

func TestFormatterRegisterRuleInParallel(t *testing.T) {
	t.Parallel()
	f, err := NewFormatter("{{json .}}", func() string { return "" }, []Rule{{Name: "seed", Description: "seeded"}})
	if err != nil {
		t.Fatal(err)
	}
	initialRules := len(f.rules.rules)

	rules := make([]Rule, 0, 100)
	for i := range 100 {
		rules = append(rules, Rule{Name: fmt.Sprintf("rule%d", i), Description: fmt.Sprintf("description for rule%d", i)})
	}

	done := make(chan struct{})
	for range 100 {
		go func() {
			for _, r := range rules {
				f.RegisterRule(r)
			}
			done <- struct{}{}
		}()
	}
	for range 100 {
		<-done
	}

	if len(f.rules.rules) != initialRules+len(rules) {
		t.Fatalf("not all rules were registered. %d rules were registered", len(f.rules.rules))
	}
}
