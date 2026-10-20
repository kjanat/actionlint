package analyzer_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

func TestScriptPosition(t *testing.T) {
	script := analyzer.Script{Text: "echo a\nx\\ny\n", Spans: []analyzer.Span{
		{Line: 1, Start: 1, End: 7, Document: diagnostic.Position{Line: 3, Column: 9}},
		{Line: 2, Start: 1, End: 2, Document: diagnostic.Position{Line: 4, Column: 9}},
		{Line: 2, Start: 2, End: 3, Document: diagnostic.Position{Line: 4, Column: 10}, DocumentEnd: diagnostic.Position{Line: 4, Column: 12}},
		{Line: 2, Start: 3, End: 4, Document: diagnostic.Position{Line: 4, Column: 12}},
		{Line: 3, Start: 1, End: 1, Document: diagnostic.Position{Line: 4, Column: 13}},
	}}
	for _, tc := range []struct {
		line, column int
		exclusive    bool
		want         diagnostic.Position
		ok           bool
	}{
		{1, 1, false, diagnostic.Position{Line: 3, Column: 9}, true},
		{1, 6, false, diagnostic.Position{Line: 3, Column: 14}, true},
		{1, 7, false, diagnostic.Position{}, false},
		{1, 7, true, diagnostic.Position{Line: 3, Column: 15}, true},
		{1, 1, true, diagnostic.Position{}, false},
		{2, 2, false, diagnostic.Position{Line: 4, Column: 10}, true},
		{2, 3, true, diagnostic.Position{Line: 4, Column: 12}, true},
		{2, 3, false, diagnostic.Position{Line: 4, Column: 12}, true},
		{3, 1, true, diagnostic.Position{Line: 4, Column: 13}, true},
		{3, 1, false, diagnostic.Position{}, false},
		{4, 1, false, diagnostic.Position{}, false},
	} {
		got, ok := script.Position(tc.line, tc.column, tc.exclusive)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Position(%d, %d, %v) = %+v, %v; want %+v, %v", tc.line, tc.column, tc.exclusive, got, ok, tc.want, tc.ok)
		}
	}
}
