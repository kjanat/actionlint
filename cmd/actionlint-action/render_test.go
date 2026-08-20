package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kjanat/actionlint"
)

func sampleProblem() *problem {
	return &problem{
		Message:   "label \"a<b>&c\" is unknown",
		Filepath:  "workflow.yaml",
		Line:      4,
		Column:    14,
		Kind:      "runner-label",
		Snippet:   "    runs-on: \"a<b>&c\"\n             ^~~~~~~~",
		EndColumn: 21,
	}
}

func TestCommandEscape(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"100%", "100%25"},
		{"a\nb", "a%0Ab"},
		{"a\r\nb", "a%0D%0Ab"},
		{"a:b,c", "a:b,c"},
		{"%0A", "%250A"},
	} {
		if got := commandEscape(tc.in); got != tc.want {
			t.Errorf("commandEscape(%q) = %q, wanted %q", tc.in, got, tc.want)
		}
	}
}

func TestPropertyEscape(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a:b,c", "a%3Ab%2Cc"},
		{"100%:", "100%25%3A"},
		{"a\nb,c", "a%0Ab%2Cc"},
	} {
		if got := propertyEscape(tc.in); got != tc.want {
			t.Errorf("propertyEscape(%q) = %q, wanted %q", tc.in, got, tc.want)
		}
	}
}

func TestProblemHeader(t *testing.T) {
	p := sampleProblem()
	if want := "workflow.yaml:4:14: label \"a<b>&c\" is unknown [runner-label]"; problemHeader(p) != want {
		t.Errorf("wanted %q but got %q", want, problemHeader(p))
	}

	p.Filepath = "::odd.yaml"
	if want := "./::odd.yaml:4:14"; !strings.HasPrefix(problemHeader(p), want) {
		t.Errorf("wanted a header starting with %q but got %q", want, problemHeader(p))
	}
}

func TestRenderDefault(t *testing.T) {
	got := renderDefault([]*problem{sampleProblem()})
	want := "workflow.yaml:4:14: label \"a<b>&c\" is unknown [runner-label]\n" +
		"  |\n" +
		"4 |     runs-on: \"a<b>&c\"\n" +
		"  |              ^~~~~~~~\n"
	if got != want {
		t.Errorf("wanted\n%q\nbut got\n%q", want, got)
	}
}

func TestRenderDefaultWithoutSnippet(t *testing.T) {
	p := sampleProblem()
	p.Snippet = ""
	got := renderDefault([]*problem{p})
	want := "workflow.yaml:4:14: label \"a<b>&c\" is unknown [runner-label]\n"
	if got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestRenderDefaultAlignsWideLineNumbers(t *testing.T) {
	p := sampleProblem()
	p.Line = 1234
	got := renderDefault([]*problem{p})
	if !strings.Contains(got, "\n     |\n1234 |     runs-on:") {
		t.Errorf("wanted the indicator gutter aligned with the line number but got\n%s", got)
	}
}

func TestRenderGitHub(t *testing.T) {
	p := sampleProblem()
	p.Filepath = "dir/work,flow:1.yaml"
	got := renderGitHub([]*problem{p})
	want := "::error file=dir/work%2Cflow%3A1.yaml,line=4,col=14,endColumn=21," +
		"title=actionlint (runner-label)::label \"a<b>&c\" is unknown%0A%0A    runs-on: \"a<b>&c\"%0A             ^~~~~~~~\n"
	if got != want {
		t.Errorf("wanted\n%q\nbut got\n%q", want, got)
	}
}

func TestRenderGitHubWithoutSnippet(t *testing.T) {
	p := sampleProblem()
	p.Snippet = ""
	got := renderGitHub([]*problem{p})
	want := "::error file=workflow.yaml,line=4,col=14,endColumn=21,title=actionlint (runner-label)::label \"a<b>&c\" is unknown\n"
	if got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestRenderOneline(t *testing.T) {
	got := renderOneline([]*problem{sampleProblem(), sampleProblem()})
	want := "workflow.yaml:4:14: label \"a<b>&c\" is unknown [runner-label]\n"
	if got != want+want {
		t.Errorf("wanted %q but got %q", want+want, got)
	}
}

func TestRenderJSONLinesKeepsLiteralHTMLCharacters(t *testing.T) {
	got, err := renderJSONLines([]*problem{sampleProblem(), sampleProblem()})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("wanted 2 lines but got %d: %q", len(lines), got)
	}
	if strings.Contains(got, `\u003c`) || !strings.Contains(got, `<`) {
		t.Errorf("wanted literal HTML characters but got %q", got)
	}
	if !strings.Contains(lines[0], `"message":"label \"a<b>&c\" is unknown"`) {
		t.Errorf("wanted a compact object with literal characters but got %q", lines[0])
	}
	var decoded problem
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.EndColumn != 21 || decoded.Kind != "runner-label" {
		t.Errorf("wanted the problem to round trip but got %#v", decoded)
	}
}

func TestRenderJSONLinesOmitsEmptyOptionalFields(t *testing.T) {
	got, err := renderJSONLines([]*problem{{Message: "m", Line: 1, Column: 2, Kind: "k", EndColumn: 2}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"message":"m","line":1,"column":2,"kind":"k","end_column":2}` + "\n"
	if got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestRenderMarkdown(t *testing.T) {
	got := renderMarkdown([]*problem{sampleProblem()})
	want := "### workflow.yaml:4:14 (runner-label)\n\nlabel \"a<b>&c\" is unknown\n\n" +
		"        runs-on: \"a<b>&c\"\n" +
		"                 ^~~~~~~~\n\n"
	if got != want {
		t.Errorf("wanted\n%q\nbut got\n%q", want, got)
	}
}

func TestRenderMarkdownWithoutSnippet(t *testing.T) {
	p := sampleProblem()
	p.Snippet = ""
	got := renderMarkdown([]*problem{p})
	want := "### workflow.yaml:4:14 (runner-label)\n\nlabel \"a<b>&c\" is unknown\n\n"
	if got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestRenderJSONPassesSerializedDocumentThrough(t *testing.T) {
	serialized := `[{"message":"m","line":1,"column":2,"kind":"k","end_column":2}]` + "\n"
	got, err := render(formatJSON, []*problem{sampleProblem()}, serialized)
	if err != nil {
		t.Fatal(err)
	}
	if got != serialized {
		t.Errorf("wanted %q but got %q", serialized, got)
	}
}

func TestRenderRejectsUnknownFormat(t *testing.T) {
	if _, err := render(formatSARIF, nil, ""); err == nil {
		t.Error("wanted an error for a format without a renderer")
	}
}

func TestRenderEmptyProblemList(t *testing.T) {
	for _, f := range []outputFormat{formatGitHub, formatDefault, formatOneline, formatJSONLines, formatMarkdown} {
		got, err := render(f, []*problem{}, "[]\n")
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("format %q: wanted an empty rendering but got %q", f, got)
		}
	}
}

func TestSARIFProblemCount(t *testing.T) {
	for _, tc := range []struct {
		document string
		want     int
	}{
		{`{"runs":[]}`, 0},
		{`{"runs":[{"results":[]}]}`, 0},
		{`{"runs":[{"results":[{"ruleId":"a"},{"ruleId":"b"}]}]}`, 2},
		{`{"runs":[{"results":[{"ruleId":"a"}]},{"results":[{"ruleId":"b"}]}]}`, 2},
	} {
		got, err := sarifProblemCount(tc.document)
		if err != nil {
			t.Fatalf("%s: %v", tc.document, err)
		}
		if got != tc.want {
			t.Errorf("%s: wanted %d but got %d", tc.document, tc.want, got)
		}
	}

	for _, document := range []string{"", "[]", `{"runs":{}}`, "not json"} {
		if _, err := sarifProblemCount(document); err == nil {
			t.Errorf("wanted an error for %q", document)
		}
	}
}

func TestRenderOutcomeCountsAndRenders(t *testing.T) {
	serialized := `[{"message":"m","filepath":"w.yaml","line":1,"column":2,"kind":"k","end_column":2}]` + "\n"
	o, count, rendered := renderOutcome(&lintOutcome{serialized, "", actionlint.ExitStatusSuccessProblemFound}, formatOneline)
	if o.code != actionlint.ExitStatusSuccessProblemFound {
		t.Errorf("wanted the exit code preserved but got %d", o.code)
	}
	if count != "1" {
		t.Errorf("wanted 1 problem but got %q", count)
	}
	if want := "w.yaml:1:2: m [k]\n"; rendered != want {
		t.Errorf("wanted %q but got %q", want, rendered)
	}
}

func TestRenderOutcomeCountsSARIFResults(t *testing.T) {
	serialized := `{"runs":[{"results":[{"ruleId":"a"}]}]}` + "\n"
	o, count, rendered := renderOutcome(&lintOutcome{serialized, "", actionlint.ExitStatusSuccessProblemFound}, formatSARIF)
	if o.code != actionlint.ExitStatusSuccessProblemFound || count != "1" {
		t.Errorf("wanted one SARIF result but got %q and code %d", count, o.code)
	}
	if rendered != serialized {
		t.Errorf("wanted the SARIF document unchanged but got %q", rendered)
	}
}

func TestRenderOutcomeReportsUnparsableOutput(t *testing.T) {
	o, count, rendered := renderOutcome(&lintOutcome{"not json", "", actionlint.ExitStatusSuccessNoProblem}, formatJSON)
	if o.code != actionlint.ExitStatusFailure {
		t.Errorf("wanted a failure exit code but got %d", o.code)
	}
	if count != "" {
		t.Errorf("wanted an empty problem count but got %q", count)
	}
	if !strings.HasPrefix(rendered, "could not parse actionlint output: ") || !strings.HasSuffix(rendered, "not json") {
		t.Errorf("wanted the parse failure and the raw output but got %q", rendered)
	}
}

func TestRenderOutcomeKeepsFailureOutput(t *testing.T) {
	o, count, rendered := renderOutcome(&lintOutcome{"partial", "boom\n", actionlint.ExitStatusFailure}, formatJSON)
	if o.code != actionlint.ExitStatusFailure || count != "" {
		t.Errorf("wanted a failure with no count but got %d and %q", o.code, count)
	}
	if want := "boom\npartial"; rendered != want {
		t.Errorf("wanted %q but got %q", want, rendered)
	}

	_, _, rendered = renderOutcome(&lintOutcome{"partial", "", actionlint.ExitStatusInvalidCommandOption}, formatJSON)
	if rendered != "partial" {
		t.Errorf("wanted %q but got %q", "partial", rendered)
	}
}

func TestEmbeddedSARIFTemplateMatchesRepository(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "format", "sarif_template.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != sarifTemplate {
		t.Error("the embedded SARIF template drifted from testdata/format/sarif_template.txt")
	}
}
