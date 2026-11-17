package enginediagnostics

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

func position(line, column int) sourcetext.Position {
	return sourcetext.Position{Line: line, Column: column}
}

func TestNormalizeConvertsEngineCoordinates(t *testing.T) {
	t.Parallel()
	// "é" is two UTF-8 bytes and one UTF-16 unit; "🐚" is four bytes and two units.
	const source = "aé🐚b\r\ncd\rxy\n"
	for _, tc := range []struct {
		name       string
		convention toolmanifest.Coordinates
		start, end sourcetext.Position
		wantStart  sourcetext.Position
		wantEnd    sourcetext.Position
	}{
		{"chars", ContractCoordinates, position(1, 4), position(1, 5), position(1, 4), position(1, 5)},
		{"bytes", toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsBytes, End: toolmanifest.EndExclusive}, position(1, 4), position(1, 8), position(1, 3), position(1, 4)},
		{"utf16", toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsUTF16, End: toolmanifest.EndExclusive}, position(1, 3), position(1, 5), position(1, 3), position(1, 4)},
		{"inclusive", toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndInclusive}, position(1, 1), position(1, 4), position(1, 1), position(1, 5)},
		{"universal newlines", UniversalNewlineCoordinates, position(2, 2), position(3, 3), position(2, 2), position(2, 6)},
		{"universal newlines bytes", toolmanifest.Coordinates{Rows: toolmanifest.RowsSource, Columns: toolmanifest.ColumnsBytes, End: toolmanifest.EndExclusive}, position(1, 8), position(3, 1), position(1, 4), position(2, 4)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := scriptengine.Diagnostic{Location: tc.start, EndLocation: tc.end, Fixes: []scriptengine.Fix{{Replacements: []scriptengine.Replacement{{Start: tc.start, End: tc.end}}}}}
			if err := NewSnippet(source, tc.convention).Normalize(&d); err != nil {
				t.Fatal(err)
			}
			replacement := d.Fixes[0].Replacements[0]
			if d.Location != tc.wantStart || d.EndLocation != tc.wantEnd || replacement.Start != tc.wantStart || replacement.End != tc.wantEnd {
				t.Fatalf("got %+v-%+v (fix %+v), want %+v-%+v", d.Location, d.EndLocation, replacement, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestNormalizeKeepsMissingEnd(t *testing.T) {
	t.Parallel()
	inclusive := toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndInclusive}
	missing := scriptengine.Diagnostic{Location: position(1, 1)}
	if err := NewSnippet("x", inclusive).Normalize(&missing); err != nil || missing.EndLocation != (sourcetext.Position{}) {
		t.Fatalf("missing end became %v (%v)", missing.EndLocation, err)
	}
}

func TestNormalizeRejectsInvalidRanges(t *testing.T) {
	t.Parallel()
	replacement := func(start, end sourcetext.Position) []scriptengine.Fix {
		return []scriptengine.Fix{{Replacements: []scriptengine.Replacement{{Start: start, End: end}}}}
	}
	for name, tc := range map[string]struct {
		d    scriptengine.Diagnostic
		want string
	}{
		"zero start":             {scriptengine.Diagnostic{EndLocation: position(1, 2)}, "invalid start"},
		"start past line end":    {scriptengine.Diagnostic{Location: position(1, 5)}, "outside the snippet"},
		"end past snippet":       {scriptengine.Diagnostic{Location: position(1, 1), EndLocation: position(3, 1)}, "outside the snippet"},
		"end at line end allows": {scriptengine.Diagnostic{Location: position(1, 1), EndLocation: position(1, 4)}, ""},
		"reversed end":           {scriptengine.Diagnostic{Location: position(2, 1), EndLocation: position(1, 3)}, "invalid range"},
		"zero end column":        {scriptengine.Diagnostic{Location: position(1, 1), EndLocation: position(1, 0)}, "invalid range"},
		"reported reversed end":  {scriptengine.Diagnostic{Source: "lib.sh", Location: position(4, 1), EndLocation: position(3, 1)}, "invalid range"},
		"fix without replace":    {scriptengine.Diagnostic{Location: position(1, 1), Fixes: []scriptengine.Fix{{}}}, "no replacements"},
		"fix without end":        {scriptengine.Diagnostic{Location: position(1, 1), Fixes: replacement(position(1, 1), sourcetext.Position{})}, "invalid range"},
		"fix reversed":           {scriptengine.Diagnostic{Location: position(1, 1), Fixes: replacement(position(1, 3), position(1, 2))}, "invalid range"},
		"fix past line end":      {scriptengine.Diagnostic{Location: position(1, 1), Fixes: replacement(position(1, 1), position(1, 5))}, "outside the snippet"},
		"fix past snippet":       {scriptengine.Diagnostic{Location: position(1, 1), Fixes: replacement(position(3, 1), position(3, 1))}, "outside the snippet"},
		"fix at line end allows": {scriptengine.Diagnostic{Location: position(1, 1), Fixes: replacement(position(1, 4), position(2, 1))}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := tc.d
			err := NewSnippet("abc\n", ContractCoordinates).Normalize(&d)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestNewSnippetCountsUniversalNewlines(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source string
		lines  []string
	}{
		{"", []string{""}},
		{"é\r\n界\ra\n", []string{"é", "界", "a", ""}},
		{"\r\r\n\n", []string{"", "", "", ""}},
		{"a b", []string{"a b"}},
	} {
		snippet := NewSnippet(tc.source, UniversalNewlineCoordinates)
		got := make([]string, len(snippet.lines))
		for i, line := range snippet.lines {
			got[i] = line.text
		}
		if strings.Join(got, "|") != strings.Join(tc.lines, "|") || len(got) != len(tc.lines) {
			t.Fatalf("%q: lines %q, want %q", tc.source, got, tc.lines)
		}
	}
}

func TestNormalizeKeepsReportedSourcePositions(t *testing.T) {
	t.Parallel()
	d := scriptengine.Diagnostic{Source: "lib.sh", Location: position(40, 3), EndLocation: position(40, 9)}
	if err := NewSnippet("a\rb\n", UniversalNewlineCoordinates).Normalize(&d); err != nil {
		t.Fatal(err)
	}
	if d.Location != position(40, 3) || d.EndLocation != position(40, 9) {
		t.Fatalf("reported source positions changed to %v-%v", d.Location, d.EndLocation)
	}
}
