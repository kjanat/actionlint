package process

import (
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
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
		convention manifests.Coordinates
		start, end checkinputs.ScriptPosition
		wantStart  checkinputs.ScriptPosition
		wantEnd    checkinputs.ScriptPosition
	}{
		{"chars", manifests.Coordinates{Rows: manifests.RowsLF, Columns: manifests.ColumnsChars, End: manifests.EndExclusive}, position(1, 4), position(1, 5), position(1, 4), position(1, 5)},
		{"bytes", manifests.Coordinates{Rows: manifests.RowsLF, Columns: manifests.ColumnsBytes, End: manifests.EndExclusive}, position(1, 4), position(1, 8), position(1, 3), position(1, 4)},
		{"utf16", manifests.Coordinates{Rows: manifests.RowsLF, Columns: manifests.ColumnsUTF16, End: manifests.EndExclusive}, position(1, 3), position(1, 5), position(1, 3), position(1, 4)},
		{"inclusive", manifests.Coordinates{Rows: manifests.RowsLF, Columns: manifests.ColumnsChars, End: manifests.EndInclusive}, position(1, 1), position(1, 4), position(1, 1), position(1, 5)},
		{"source rows", manifests.Coordinates{Rows: manifests.RowsSource, Columns: manifests.ColumnsChars, End: manifests.EndExclusive}, position(2, 2), position(3, 3), position(2, 2), position(2, 6)},
		{"source rows bytes", manifests.Coordinates{Rows: manifests.RowsSource, Columns: manifests.ColumnsBytes, End: manifests.EndExclusive}, position(1, 8), position(3, 1), position(1, 4), position(2, 4)},
		{"past line end", manifests.Coordinates{Rows: manifests.RowsLF, Columns: manifests.ColumnsBytes, End: manifests.EndExclusive}, position(3, 9), position(9, 1), position(3, 9), position(9, 1)},
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
	c := newCoordinates(source, &manifests.Coordinates{Rows: manifests.RowsLF, Columns: manifests.ColumnsChars, End: manifests.EndInclusive})
	if end := c.end(checkinputs.ScriptPosition{}); end != (checkinputs.ScriptPosition{}) {
		t.Fatalf("missing end became %+v", end)
	}
}
