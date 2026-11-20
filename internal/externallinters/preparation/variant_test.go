package preparation

import "testing"

func TestVariantColumns(t *testing.T) {
	t.Parallel()
	variant := Variant{Source: "tar xy x  y\n", Substitutions: []Substitution{
		{Template: "${{ a }}", Value: "xy", Row: 1, Column: 5, Alternatives: 2},
		{Template: "${{ b }}", Value: "", Row: 1, Column: 16, Alternatives: 2},
	}}
	for _, tc := range []struct {
		name        string
		row, column int
		end         bool
		want        int
	}{
		{"before values", 1, 1, false, 1},
		{"value start", 1, 5, false, 5},
		{"inside value", 1, 6, false, 5},
		{"after value", 1, 7, false, 13},
		{"between values", 1, 8, false, 14},
		{"after empty value", 1, 10, false, 24},
		{"last column", 1, 11, false, 25},
		{"end at value start", 1, 5, true, 5},
		{"end inside value", 1, 6, true, 13},
		{"end after value", 1, 7, true, 13},
		{"end at empty value", 1, 10, true, 16},
		{"other row", 2, 7, false, 7},
	} {
		got := variant.StartColumn(tc.row, tc.column)
		if tc.end {
			got = variant.EndColumn(tc.row, tc.column)
		}
		if got != tc.want {
			t.Errorf("%s: row %d column %d mapped to %d, want %d", tc.name, tc.row, tc.column, got, tc.want)
		}
	}
}
