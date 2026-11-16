package process

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

func position(row, column int) checkinputs.ScriptPosition {
	return checkinputs.ScriptPosition{Row: row, Column: column}
}

func TestCoordinates(t *testing.T) {
	t.Parallel()
	// "é" is two UTF-8 bytes and one UTF-16 unit; "🐚" is four bytes and two units.
	const source = "aé🐚b\r\ncd\rxy\n"
	for _, tc := range []struct {
		name       string
		convention toolmanifest.Coordinates
		start, end checkinputs.ScriptPosition
		wantStart  checkinputs.ScriptPosition
		wantEnd    checkinputs.ScriptPosition
	}{
		{"chars", toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndExclusive}, position(1, 4), position(1, 5), position(1, 4), position(1, 5)},
		{"bytes", toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsBytes, End: toolmanifest.EndExclusive}, position(1, 4), position(1, 8), position(1, 3), position(1, 4)},
		{"utf16", toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsUTF16, End: toolmanifest.EndExclusive}, position(1, 3), position(1, 5), position(1, 3), position(1, 4)},
		{"inclusive", toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndInclusive}, position(1, 1), position(1, 4), position(1, 1), position(1, 5)},
		{"source rows", toolmanifest.Coordinates{Rows: toolmanifest.RowsSource, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndExclusive}, position(2, 2), position(3, 3), position(2, 2), position(2, 6)},
		{"source rows bytes", toolmanifest.Coordinates{Rows: toolmanifest.RowsSource, Columns: toolmanifest.ColumnsBytes, End: toolmanifest.EndExclusive}, position(1, 8), position(3, 1), position(1, 4), position(2, 4)},
		{"past line end", toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsBytes, End: toolmanifest.EndExclusive}, position(3, 9), position(9, 1), position(3, 9), position(9, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newCoordinates(source, &tc.convention)
			d := checkinputs.ScriptDiagnostic{Location: tc.start, EndLocation: tc.end, Fixes: []checkinputs.ScriptFix{{Replacements: []checkinputs.ScriptReplacement{{Row: tc.start.Row, Column: tc.start.Column, EndRow: tc.end.Row, EndColumn: tc.end.Column}}}}}
			c.diagnostic(&d)
			replacement := d.Fixes[0].Replacements[0]
			if d.Location != tc.wantStart || d.EndLocation != tc.wantEnd || position(replacement.Row, replacement.Column) != tc.wantStart || position(replacement.EndRow, replacement.EndColumn) != tc.wantEnd {
				t.Fatalf("got %+v-%+v (fix %+v), want %+v-%+v", d.Location, d.EndLocation, replacement, tc.wantStart, tc.wantEnd)
			}
		})
	}
	c := newCoordinates(source, &toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndInclusive})
	if end := c.end(checkinputs.ScriptPosition{}); end != (checkinputs.ScriptPosition{}) {
		t.Fatalf("missing end became %+v", end)
	}
}
