package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/buildinfo"
	"actionlint.kjanat.dev/internal/rulecatalog"

	"github.com/fatih/color"
	"github.com/google/go-cmp/cmp"
)

func init() {
	color.NoColor = true
}

type testErrorWriter struct{}

func (w testErrorWriter) Write(b []byte) (int, error) {
	return 0, errors.New("dummy write error")
}

func TestErrorPrettyPrint(t *testing.T) {
	testCases := []struct {
		message  string
		line     int
		column   int
		kind     string
		expected string
		source   string
	}{
		{
			message:  "simple message",
			line:     1,
			column:   1,
			expected: "filename.txt:1:1: simple message [kind]",
		},
		{
			message: "simple message with source",
			line:    1,
			column:  1,
			source:  "this is source",
			expected: `filename.txt:1:1: simple message with source [kind]
  |
1 | this is source
  | ^~~~`,
		},
		{
			message: "error at middle of source",
			line:    1,
			column:  6,
			source:  "this is source",
			expected: `filename.txt:1:6: error at middle of source [kind]
  |
1 | this is source
  |      ^~`,
		},
		{
			message: "error at end of source",
			line:    1,
			column:  15,
			source:  "this is source",
			expected: `filename.txt:1:15: error at end of source [kind]
  |
1 | this is source
  |               ^`,
		},
		{
			message: "error at one character word in source",
			line:    1,
			column:  5,
			source:  "foo . bar",
			expected: `filename.txt:1:5: error at one character word in source [kind]
  |
1 | foo . bar
  |     ^`,
		},
		{
			message: "error at space in source",
			line:    1,
			column:  4,
			source:  "foo bar",
			expected: `filename.txt:1:4: error at space in source [kind]
  |
1 | foo bar
  |    ^`,
		},
		{
			message: "simple message with multi-line source",
			line:    3,
			column:  3,
			source:  "this\nis\nsource",
			expected: `filename.txt:3:3: simple message with multi-line source [kind]
  |
3 | source
  |   ^~~~`,
		},
		{
			message: "error at end of multi-line source",
			line:    3,
			column:  7,
			source:  "this\nis\nsource",
			expected: `filename.txt:3:7: error at end of multi-line source [kind]
  |
3 | source
  |       ^`,
		},
		{
			message: "error at newline of multi-line source",
			line:    2,
			column:  3,
			source:  "this\nis\nsource",
			expected: `filename.txt:2:3: error at newline of multi-line source [kind]
  |
2 | is
  |   ^`,
		},
		{
			message: "error at blank line of multi-line source",
			line:    2,
			column:  1,
			source:  "this\n\nsource",
			expected: `filename.txt:2:1: error at blank line of multi-line source [kind]
  |
2 | 
  | ^`,
		},
		{
			message: "error at line more than 10",
			line:    11,
			column:  2,
			source:  "\n\n\n\n\n\n\n\n\n\nfooo",
			expected: `filename.txt:11:2: error at line more than 10 [kind]
   |
11 | fooo
   |  ^~~`,
		},
		{
			message:  "error at out of source",
			line:     2,
			column:   6,
			source:   "aaa\nbbb\nccc",
			expected: "filename.txt:2:6: error at out of source [kind]",
		},
		{
			message: "error at zero column",
			line:    1,
			column:  0,
			source:  "this is source",
			expected: `filename.txt:1:0: error at zero column [kind]
  |
1 | this is source
  | `,
		},
		{
			message:  "error at zero line and zero column",
			line:     0,
			column:   0,
			source:   "this is source",
			expected: `filename.txt:0:0: error at zero line and zero column [kind]`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.message, func(t *testing.T) {
			err := testFinding(&diagmodel.Position{Line: tc.line, Column: tc.column}, "kind", tc.message)
			err.Diagnostic.Path = "filename.txt"

			var buf bytes.Buffer
			testPrettyPrint(err, &buf, []byte(tc.source))

			out := buf.String()
			want := tc.expected + "\n"
			if out != want {
				t.Fatalf(`wanted:
%q

have:
%q`, want, out)
			}
		})
	}
}

func TestErrorGetTemplateFieldsOK(t *testing.T) {
	testCases := []struct {
		message string
		column  int
		endCol  int
		source  string
		snippet string
	}{
		{
			message: "simple message with source",
			column:  1,
			endCol:  4,
			source:  "this is source",
			snippet: "this is source\n^~~~",
		},
		{
			message: "simple message",
			column:  1,
			endCol:  1,
			snippet: "",
		},
		{
			message: "error at zero column",
			column:  0,
			endCol:  0,
			source:  "this is source",
			snippet: "this is source",
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			err := testFinding(&diagmodel.Position{Line: 1, Column: tc.column}, "kind", tc.message)
			err.Diagnostic.Path = "filename.txt"
			f := testFields(err, []byte(tc.source))
			if f.Message != tc.message {
				t.Fatalf("wanted %q but have %q", tc.message, f.Message)
			}
			if f.Line != err.Diagnostic.Start.Line {
				t.Fatalf("wanted %d but have %d", err.Diagnostic.Start.Line, f.Line)
			}
			if f.Column != tc.column {
				t.Fatalf("wanted %d but have %d", tc.column, f.Column)
			}
			if f.Filepath != err.Diagnostic.Path {
				t.Fatalf("wanted %q but have %q", err.Diagnostic.Path, f.Filepath)
			}
			if f.Kind != err.Diagnostic.Rule {
				t.Fatalf("wanted %q but have %q", err.Diagnostic.Rule, f.Kind)
			}
			if f.Snippet != tc.snippet {
				t.Fatalf("wanted %q but have %q", tc.snippet, f.Snippet)
			}
			if f.EndColumn != tc.endCol {
				t.Fatalf("wanted %d but have %d", tc.endCol, f.EndColumn)
			}
		})
	}
}

// Regression test for #128
func TestErrorGetTemplateFieldsColumnIsOutOfBounds(t *testing.T) {
	err := testFinding(&diagmodel.Position{Line: 1, Column: 9999}, "kind", "this is message")
	err.Diagnostic.Path = "filename.yaml"
	f := testFields(err, []byte("this is source"))
	if strings.Contains(f.Snippet, "\n") {
		t.Fatalf("snippet should contain indicator but it has: %q", f.Snippet)
	}
}

func TestErrorGetTemplateFieldsExplicitEndColumn(t *testing.T) {
	err := testFinding(&diagmodel.Position{Line: 1, Column: 6}, "kind", "this is message")
	err.EndColumn = 9
	f := testFields(err, []byte("echo $foo bar"))
	if f.Snippet != "echo $foo bar\n     ^~~~" {
		t.Fatalf("unexpected snippet: %q", f.Snippet)
	}
	if f.EndColumn != 9 {
		t.Fatalf("end column is %d but wanted 9", f.EndColumn)
	}
}

func TestErrorGetTemplateFieldsExplicitEndColumnAfterUnicode(t *testing.T) {
	err := testFinding(&diagmodel.Position{Line: 1, Column: 8}, "kind", "this is message")
	err.EndColumn = 11
	f := testFields(err, []byte("echo é $foo"))
	if f.Snippet != "echo é $foo\n       ^~~~" {
		t.Fatalf("unexpected snippet: %q", f.Snippet)
	}
	if f.EndColumn != 11 {
		t.Fatalf("end column is %d but wanted 11", f.EndColumn)
	}
}

func TestErrorGetTemplateFieldsEndColumnAfterWideRunes(t *testing.T) {
	err := testFinding(&diagmodel.Position{Line: 1, Column: 5}, "kind", "this is message")
	f := testFields(err, []byte("日本語 $foo"))
	if f.Snippet != "日本語 $foo\n       ^~~~" {
		t.Fatalf("unexpected snippet: %q", f.Snippet)
	}
	if f.EndColumn != 8 {
		t.Fatalf("end column is %d but wanted 8", f.EndColumn)
	}
}

var testErrorTemplateFields = []*TemplateFields{
	{
		Message:   "message 1",
		Filepath:  "file1",
		Line:      1,
		Column:    2,
		EndColumn: 3,
		Snippet:   "snippet 1",
		Kind:      "kind1",
	},
	{
		Message:   "message 2",
		Filepath:  "file2",
		Line:      3,
		Column:    4,
		EndColumn: 5,
		Snippet:   "snippet 2",
		Kind:      "kind2",
	},
}

func TestErrorPrintFormattedWithTemplateFields(t *testing.T) {
	testCases := []struct {
		temp string
		want string
	}{
		{
			temp: "{{(index . 0).Message}} {{(index . 1).Message}}",
			want: "message 1 message 2",
		},
		{
			temp: "{{range $ = .}}({{$.Line}}, {{$.Column}}){{end}}",
			want: "(1, 2)(3, 4)",
		},
		{
			temp: "{{range $ = .}}[{{$.Column}}..{{$.EndColumn}}]{{end}}",
			want: "[2..3][4..5]",
		},
		{
			temp: "{{range $ = .}}{{json $.Snippet}}{{end}}",
			want: "\"snippet 1\"\n\"snippet 2\"\n",
		},
		{
			temp: "{{range $ = .}}{{replace $.Kind \"kind\" \"king\"}}{{end}}",
			want: "king1king2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.temp, func(t *testing.T) {
			f, err := newTestFormatter(tc.temp)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			if err := f.Print(&b, testErrorTemplateFields); err != nil {
				t.Fatal(err)
			}
			have := b.String()
			if tc.want != have {
				t.Fatalf("wanted %q but have %q", tc.want, have)
			}
		})
	}
}

func TestErrorPrintFormattedErrors(t *testing.T) {
	errs := []*Finding{
		testFinding(&diagmodel.Position{Line: 1, Column: 1}, "kind1", "error1"),
		testFinding(&diagmodel.Position{Line: 1, Column: 0}, "kind2", "error2"),
	}

	f, err := newTestFormatter("{{range $ = .}}({{$.Message | printf \"%q\"}},{{$.Snippet | printf \"%q\"}}){{end}}")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := f.Print(&b, []*TemplateFields{testFields(errs[0], []byte("this is source")), testFields(errs[1], []byte("this is source"))}); err != nil {
		t.Fatal(err)
	}
	have := b.String()
	want := `("error1","this is source\n^~~~")("error2","this is source")`
	if want != have {
		t.Fatalf("wanted %q but have %q", want, have)
	}
}

func TestErrorPrintSerializedIntoJSON(t *testing.T) {
	f, err := newTestFormatter("{{json .}}")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := f.Print(&b, testErrorTemplateFields); err != nil {
		t.Fatal(err)
	}

	decoded := []*TemplateFields{}
	if err := json.Unmarshal(b.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(testErrorTemplateFields, decoded); diff != "" {
		t.Fatal(diff)
	}
}

func TestErrorPrintAllKinds(t *testing.T) {
	f, err := newTestFormatter("{{range $ = allKinds}}{{$.Name}}: {{$.Description}}\n{{end}}")
	if err != nil {
		t.Fatal(err)
	}

	f.RegisterRule(Rule{Name: "rule1", Description: "description for rule1"})
	f.RegisterRule(Rule{Name: "rule2", Description: "description for rule2"})

	var b bytes.Buffer
	if err := f.Print(&b, nil); err != nil {
		t.Fatal(err)
	}
	output := b.String()

	for _, want := range []string{
		"syntax-check: Validate GitHub Actions workflow syntax\n",
		"inline-suppression: Validate inline suppression directives\n",
		"disallow-suppressions: Report suppressions forbidden by policy\n",
		"rule1: description for rule1\n",
		"rule2: description for rule2\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("%q is not included in `allKinds` output: %q", want, output)
		}
	}
}

func TestErrorNewErrorFormatterError(t *testing.T) {
	testCases := []struct {
		temp string
		want string
	}{
		{"hello", "template to format error messages must contain at least one {{ }} placeholder"},
		{"{{xxx", "template \"{{xxx\" to format error messages could not be parsed"},
	}

	for _, tc := range testCases {
		t.Run(tc.temp, func(t *testing.T) {
			_, err := newTestFormatter(tc.temp)
			if err == nil {
				t.Fatal("error did not occur")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%q is not contained in error message %q", tc.want, err.Error())
			}
		})
	}
}

func TestErrorFormatterPrintError(t *testing.T) {
	testCases := []struct {
		out  io.Writer
		temp string
		want string
	}{
		{io.Discard, "{{.Foo}}", "can't evaluate field Foo in type"},
		{testErrorWriter{}, "{{(index . 0).Message}}", "dummy write error"},
	}

	for _, tc := range testCases {
		t.Run(tc.temp, func(t *testing.T) {
			f, err := newTestFormatter(tc.temp)
			if err != nil {
				t.Fatal(err)
			}
			err = f.Print(tc.out, testErrorTemplateFields)
			if err == nil {
				t.Fatal("error did not occur")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%q is not contained in error message %q", tc.want, err.Error())
			}
		})
	}
}

func TestErrorFormatterPrintToPascalCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"foo", "Foo"},
		{"Foo", "Foo"},
		{"foo-bar", "FooBar"},
		{"fooBar", "FooBar"},
		{"FooBar", "FooBar"},
		{"foo_bar", "FooBar"},
		{"foo.bar", "FooBar"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			f, err := newTestFormatter("{{range $ = .}}{{toPascalCase .Message}}{{end}}")
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder

			fs := []*TemplateFields{{Message: tc.input}}
			if err := f.Print(&b, fs); err != nil {
				t.Fatal(err)
			}

			have := b.String()
			if have != tc.want {
				t.Fatalf("wanted %q but have %q", tc.want, have)
			}
		})
	}
}

func TestErrorFormatterPrintGetVersion(t *testing.T) {
	saved := buildinfo.ReleaseVersion
	defer func() {
		buildinfo.ReleaseVersion = saved
	}()
	buildinfo.ReleaseVersion = "dummy version"

	f, err := newTestFormatter("{{getVersion}}")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder

	if err := f.Print(&b, []*TemplateFields{}); err != nil {
		t.Fatal(err)
	}

	out := b.String()
	if out != "dummy version" {
		t.Fatalf("version should be \"dummy version\" but have %q", out)
	}
}

// Fixtures construct presentation inputs directly; no engine or compatibility API is involved.
func testFinding(pos *diagmodel.Position, kind, message string) *Finding {
	return &Finding{Diagnostic: diagmodel.Diagnostic{Start: *pos, Rule: kind, Message: message}}
}
func testFields(f *Finding, source []byte) *TemplateFields {
	f.Source = source
	return f.TemplateFields()
}
func testPrettyPrint(f *Finding, out io.Writer, source []byte) { f.Source = source; f.PrettyPrint(out) }
func newTestFormatter(format string) (*Formatter, error) {
	return NewFormatter(format, buildinfo.Version, rulecatalog.Rules())
}
