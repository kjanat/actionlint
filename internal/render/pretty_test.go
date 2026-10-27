package render

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

func TestPrettyDiagnostic(t *testing.T) {
	r, err := New(FormatPretty, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := diagnostic.Diagnostic{
		Severity: "warning", Rule: "expression-reference", Message: "unknown variable",
		Path: "ci.yml", Start: diagnostic.Position{Line: 1, Column: 3}, End: diagnostic.Position{Line: 1, Column: 6},
		Help: "Use a declared value.", Related: []diagnostic.Related{{Message: "declaration", Path: "other.yml", Start: diagnostic.Position{Line: 4, Column: 2}}},
	}
	var out strings.Builder
	if err := r.Render(&out, Result{Findings: []Finding{FindingOf(d, []byte("  bad"))}}); err != nil {
		t.Fatal(err)
	}
	want := "warning[expression-reference]\n  unknown variable\n  --> ci.yml:1:3\n  |\n1 |   bad\n  |   ^^^\n  = help: Use a declared value.\n  = related: declaration\n    --> other.yml:4:2\n\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", &out, want)
	}
	if err := r.Render(failingRenderWriter{}, Result{Findings: []Finding{FindingOf(d, nil)}}); err == nil {
		t.Fatal("lost write failure")
	}
}

func TestPrettyWidthAndCoordinates(t *testing.T) {
	for _, tc := range []struct {
		line          string
		column, end   int
		want          string
		start, length int
	}{
		{"\t界e\u0301!", 3, 4, "    界e\u0301!", 6, 1},
		{strings.Repeat("a", 80) + "bad", 81, 84, "..." + strings.Repeat("a", 10) + "bad", 13, 3},
		{"abc", 4, 4, "abc", 3, 1},
		{"abc", 8, 8, "abc", -1, 1},
	} {
		f := FindingOf(diagnostic.Diagnostic{Start: diagnostic.Position{Line: 1, Column: tc.column}, End: diagnostic.Position{Line: 1, Column: tc.end}}, []byte(tc.line))
		line, start, length := f.prettySource(tc.line, 30)
		if line != tc.want || start != tc.start || length != tc.length {
			t.Errorf("%q: got %q %d %d, want %q %d %d", tc.line, line, start, length, tc.want, tc.start, tc.length)
		}
	}
	var out strings.Builder
	f := FindingOf(diagnostic.Diagnostic{
		Rule: "long-rule", Message: strings.Repeat("界", 100) + "\nnext paragraph",
		Path: strings.Repeat("path/", 30), Help: "escape\x1b[31m must not execute",
	}, nil)
	if err := f.printPretty(&out, 40); err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(out.String(), "\n") {
		if runewidth.StringWidth(line) > 40 {
			t.Errorf("line exceeds width: %q", line)
		}
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("untrusted terminal escape")
	}
}

func TestPrettyColorAndUnavailableSource(t *testing.T) {
	previous := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = previous })
	var out strings.Builder
	f := FindingOf(diagnostic.Diagnostic{Severity: "warning", Message: "message", Path: "file.yml", Start: diagnostic.Position{Line: 8, Column: 4}}, nil)
	if err := f.printPretty(&out, 80); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatal("missing color")
	}
	if strings.Contains(out.String(), " |") {
		t.Fatal("invented unavailable source")
	}
}
