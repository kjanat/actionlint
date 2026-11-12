package text

import (
	"math"
	"testing"
)

func TestTokenEndColumn(t *testing.T) {
	for _, tc := range []struct {
		line       string
		start, end int
	}{
		{"écho world", 1, 5},
		{"écho world", 6, 11},
		{"écho world", 5, 6},
		{"abc", 4, 5},
		{"abc", 20, 21},
		{"abc", 0, 1},
		{"", 1, 2},
		{"abc\tdef", 1, 4},
		{"abc\r", 1, 4},
		{"ab\u2028cd", 1, 6},
		{"ab\u00a0cd", 1, 6},
		{"ab\ncd", 1, 3},
		{"ab\tcd", 3, 4},
		{"🐚ab x", 1, 4},
		{"a\xffb x", 1, 4},
	} {
		if end := TokenEndColumn(tc.line, tc.start); end != tc.end {
			t.Errorf("%q column %d: end %d, want %d", tc.line, tc.start, end, tc.end)
		}
	}
}

func TestByteOffsetAtColumn(t *testing.T) {
	for _, tc := range []struct {
		text   string
		column int
		offset int
		ok     bool
	}{
		{"", 1, 0, true},
		{"", 2, 0, false},
		{"abc", 1, 0, true},
		{"abc", 3, 2, true},
		{"abc", 4, 3, true},
		{"abc", 5, 0, false},
		{"é🐚x", 2, 2, true},
		{"é🐚x", 3, 6, true},
		{"é🐚x", 4, 7, true},
		{"é🐚x", 5, 0, false},
		{"a\xffb", 3, 2, true},
		{"abc", 0, 0, false},
		{"abc", -1, 0, false},
		{"abc", math.MinInt, 0, false},
		{"abc", math.MaxInt, 0, false},
	} {
		if offset, ok := ByteOffsetAtColumn(tc.text, tc.column); offset != tc.offset || ok != tc.ok {
			t.Errorf("ByteOffsetAtColumn(%q, %d) = (%d, %v), want (%d, %v)", tc.text, tc.column, offset, ok, tc.offset, tc.ok)
		}
	}
}

func TestInclusiveEnd(t *testing.T) {
	for _, tc := range []struct {
		line                  string
		start, explicit, want int
	}{
		{"abc def", 1, 0, 3},
		{"abc def", 5, 0, 7},
		{"abc def", 1, 2, 2},
		{"abc def", 1, 1, 1},
		{"abc def", 3, 2, 3},
		{"abc def", 4, 0, 4},
		{"é🐚x next", 1, 0, 3},
		{"é🐚x next", 2, 2, 2},
	} {
		if got := InclusiveEnd(tc.line, tc.start, tc.explicit); got != tc.want {
			t.Errorf("InclusiveEnd(%q, %d, %d) = %d, want %d", tc.line, tc.start, tc.explicit, got, tc.want)
		}
	}
}
