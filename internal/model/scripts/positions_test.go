package scripts_test

import (
	"testing"

	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestDeclarationPosition(t *testing.T) {
	script := scriptmodel.Declaration{Text: "echo a\nx\\ny\n", Spans: []scriptmodel.Span{
		{Line: 1, Start: 1, End: 7, Document: results.Position{Line: 3, Column: 9}},
		{Line: 2, Start: 1, End: 2, Document: results.Position{Line: 4, Column: 9}},
		{Line: 2, Start: 2, End: 3, Document: results.Position{Line: 4, Column: 10}, DocumentEnd: results.Position{Line: 4, Column: 12}},
		{Line: 2, Start: 3, End: 4, Document: results.Position{Line: 4, Column: 12}},
		{Line: 3, Start: 1, End: 1, Document: results.Position{Line: 4, Column: 13}},
	}}
	for _, tc := range []struct {
		line, column int
		exclusive    bool
		want         results.Position
		ok           bool
	}{
		{1, 1, false, results.Position{Line: 3, Column: 9}, true},
		{1, 6, false, results.Position{Line: 3, Column: 14}, true},
		{1, 7, false, results.Position{}, false},
		{1, 7, true, results.Position{Line: 3, Column: 15}, true},
		{1, 1, true, results.Position{}, false},
		{2, 2, false, results.Position{Line: 4, Column: 10}, true},
		{2, 3, true, results.Position{Line: 4, Column: 12}, true},
		{2, 3, false, results.Position{Line: 4, Column: 12}, true},
		{3, 1, true, results.Position{Line: 4, Column: 13}, true},
		{3, 1, false, results.Position{}, false},
		{4, 1, false, results.Position{}, false},
	} {
		got, ok := script.Position(tc.line, tc.column, tc.exclusive)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Position(%d, %d, %v) = %+v, %v; want %+v, %v", tc.line, tc.column, tc.exclusive, got, ok, tc.want, tc.ok)
		}
	}
}
