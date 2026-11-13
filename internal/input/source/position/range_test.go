package position

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

func TestStrictCapturedRanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		model      sourcepos.Coordinates
		content    string
		start, end sourcepos.Position
		valid      bool
		a, b       int
	}{
		{"yaml CRLF", sourcepos.YAML, "a\r\n", sourcepos.Position{Line: 1, Column: 2}, sourcepos.Position{Line: 2, Column: 1}, true, 1, 3},
		{"yaml CRLF interior", sourcepos.YAML, "a\r\n", sourcepos.Position{Line: 1, Column: 3}, sourcepos.Position{Line: 2, Column: 1}, false, 0, 0},
		{"raw CR scalar", sourcepos.RawLF, "a\r\n", sourcepos.Position{Line: 1, Column: 2}, sourcepos.Position{Line: 1, Column: 3}, true, 1, 2},
		{"EOF point", sourcepos.YAML, "a\r\n", sourcepos.Position{Line: 2, Column: 1}, sourcepos.Position{Line: 2, Column: 1}, true, 3, 3},
		{"past EOF", sourcepos.YAML, "a\r\n", sourcepos.Position{Line: 2, Column: 1}, sourcepos.Position{Line: 2, Column: 2}, false, 0, 0},
		{"reversed", sourcepos.RawLF, "abc", sourcepos.Position{Line: 1, Column: 3}, sourcepos.Position{Line: 1, Column: 2}, false, 0, 0},
		{"zero end", sourcepos.YAML, "abc", sourcepos.Position{Line: 1, Column: 1}, sourcepos.Position{}, false, 0, 0},
		{"BOM Unicode", sourcepos.YAML, "\ufeff😀x", sourcepos.Position{Line: 1, Column: 1}, sourcepos.Position{Line: 1, Column: 2}, true, 3, 7},
		{"raw BOM scalar", sourcepos.RawLF, "\ufeff😀x", sourcepos.Position{Line: 1, Column: 1}, sourcepos.Position{Line: 1, Column: 2}, true, 0, 3},
		{"invalid UTF8", sourcepos.RawLF, string([]byte{0xff}), sourcepos.Position{Line: 1, Column: 1}, sourcepos.Position{Line: 1, Column: 1}, false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, b, err := Range([]byte(tc.content), tc.model, tc.start, tc.end)
			if (err == nil) != tc.valid || tc.valid && (a != tc.a || b != tc.b) {
				t.Fatalf("range=(%d,%d) err=%v", a, b, err)
			}
		})
	}
}
