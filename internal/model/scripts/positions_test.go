package scripts_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestDeclarationPosition(t *testing.T) {
	t.Parallel()
	// "echo a" and its line feed are copied, "\t" is a two-character escape, and
	// the final line feed is synthesized.
	script := scripts.Declaration{Text: "echo a\nx\ty\n", Spans: []sourcemap.Span{
		{Start: 0, End: 7, Document: source.Position{Line: 3, Column: 9}},
		{Start: 7, End: 8, Document: source.Position{Line: 4, Column: 9}},
		{Start: 8, End: 9, Document: source.Position{Line: 4, Column: 10}, DocumentEnd: source.Position{Line: 4, Column: 12}},
		{Start: 9, End: 10, Document: source.Position{Line: 4, Column: 12}},
	}}
	for _, tc := range []struct {
		line, column int
		exclusive    bool
		want         source.Position
		ok           bool
	}{
		{1, 1, false, source.Position{Line: 3, Column: 9}, true},
		{1, 6, false, source.Position{Line: 3, Column: 14}, true},
		{1, 7, false, source.Position{Line: 3, Column: 15}, true},
		{1, 7, true, source.Position{Line: 3, Column: 15}, true},
		{1, 8, false, source.Position{}, false},
		{1, 1, true, source.Position{}, false},
		{2, 2, false, source.Position{Line: 4, Column: 10}, true},
		{2, 3, true, source.Position{Line: 4, Column: 12}, true},
		{2, 3, false, source.Position{Line: 4, Column: 12}, true},
		{2, 4, false, source.Position{}, false},
		{3, 1, true, source.Position{Line: 4, Column: 13}, true},
		{3, 1, false, source.Position{}, false},
		{4, 1, false, source.Position{}, false},
		{0, 1, false, source.Position{}, false},
	} {
		got, ok := script.Position(tc.line, tc.column, tc.exclusive)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Position(%d, %d, %v) = %+v, %v; want %+v, %v", tc.line, tc.column, tc.exclusive, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDeclarationOffsetPosition(t *testing.T) {
	t.Parallel()
	script := scripts.Declaration{Text: "echo é\nx\n", Spans: []sourcemap.Span{
		{Start: 0, End: 7, Document: source.Position{Line: 3, Column: 9}},
		{Start: 8, End: 9, Document: source.Position{Line: 4, Column: 9}},
	}}
	for _, tc := range []struct {
		offset    int
		exclusive bool
		want      source.Position
		ok        bool
	}{
		{0, false, source.Position{Line: 3, Column: 9}, true},
		{5, false, source.Position{Line: 3, Column: 14}, true},
		{7, true, source.Position{Line: 3, Column: 15}, true},
		{8, false, source.Position{Line: 4, Column: 9}, true},
		{9, true, source.Position{Line: 4, Column: 10}, true},
		{10, false, source.Position{}, false},
	} {
		got, ok := script.OffsetPosition(tc.offset, tc.exclusive)
		if ok != tc.ok || got != tc.want {
			t.Errorf("OffsetPosition(%d, %v) = %+v, %v; want %+v, %v", tc.offset, tc.exclusive, got, ok, tc.want, tc.ok)
		}
	}
}
