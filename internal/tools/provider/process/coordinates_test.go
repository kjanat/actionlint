package process

import (
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

func position(row, column int) analyzer.ScriptPosition {
	return analyzer.ScriptPosition{Row: row, Column: column}
}

func TestCoordinates(t *testing.T) {
	// "é" is two UTF-8 bytes and one UTF-16 unit; "🐚" is four bytes and two units.
	const source = "aé🐚b\r\ncd\rxy\n"
	for _, tc := range []struct {
		name       string
		convention provider.Coordinates
		start, end analyzer.ScriptPosition
		wantStart  analyzer.ScriptPosition
		wantEnd    analyzer.ScriptPosition
	}{
		{"chars", provider.Coordinates{Rows: provider.RowsLF, Columns: provider.ColumnsChars, End: provider.EndExclusive}, position(1, 4), position(1, 5), position(1, 4), position(1, 5)},
		{"bytes", provider.Coordinates{Rows: provider.RowsLF, Columns: provider.ColumnsBytes, End: provider.EndExclusive}, position(1, 4), position(1, 8), position(1, 3), position(1, 4)},
		{"utf16", provider.Coordinates{Rows: provider.RowsLF, Columns: provider.ColumnsUTF16, End: provider.EndExclusive}, position(1, 3), position(1, 5), position(1, 3), position(1, 4)},
		{"inclusive", provider.Coordinates{Rows: provider.RowsLF, Columns: provider.ColumnsChars, End: provider.EndInclusive}, position(1, 1), position(1, 4), position(1, 1), position(1, 5)},
		{"source rows", provider.Coordinates{Rows: provider.RowsSource, Columns: provider.ColumnsChars, End: provider.EndExclusive}, position(2, 2), position(3, 3), position(2, 2), position(2, 6)},
		{"source rows bytes", provider.Coordinates{Rows: provider.RowsSource, Columns: provider.ColumnsBytes, End: provider.EndExclusive}, position(1, 8), position(3, 1), position(1, 4), position(2, 4)},
		{"past line end", provider.Coordinates{Rows: provider.RowsLF, Columns: provider.ColumnsBytes, End: provider.EndExclusive}, position(3, 9), position(9, 1), position(3, 9), position(9, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCoordinates(source, &tc.convention)
			d := analyzer.ScriptDiagnostic{Location: tc.start, EndLocation: tc.end, Fixes: []analyzer.ScriptFix{{Replacements: []analyzer.ScriptReplacement{{Row: tc.start.Row, Column: tc.start.Column, EndRow: tc.end.Row, EndColumn: tc.end.Column}}}}}
			c.diagnostic(&d)
			replacement := d.Fixes[0].Replacements[0]
			if d.Location != tc.wantStart || d.EndLocation != tc.wantEnd || position(replacement.Row, replacement.Column) != tc.wantStart || position(replacement.EndRow, replacement.EndColumn) != tc.wantEnd {
				t.Fatalf("got %+v-%+v (fix %+v), want %+v-%+v", d.Location, d.EndLocation, replacement, tc.wantStart, tc.wantEnd)
			}
		})
	}
	c := newCoordinates(source, &provider.Coordinates{Rows: provider.RowsLF, Columns: provider.ColumnsChars, End: provider.EndInclusive})
	if end := c.end(analyzer.ScriptPosition{}); end != (analyzer.ScriptPosition{}) {
		t.Fatalf("missing end became %+v", end)
	}
}
