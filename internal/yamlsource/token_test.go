package yamlsource

import "testing"

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
	} {
		if end := TokenEndColumn(tc.line, tc.start); end != tc.end {
			t.Errorf("%q column %d: end %d, want %d", tc.line, tc.start, end, tc.end)
		}
	}
}
