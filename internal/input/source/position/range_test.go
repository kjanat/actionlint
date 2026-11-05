package position

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestStrictCapturedRanges(t *testing.T) {
	for _, tc := range []struct {
		name       string
		model      source.Coordinates
		content    string
		start, end results.Position
		valid      bool
		a, b       int
	}{
		{"yaml CRLF", source.YAML, "a\r\n", results.Position{Line: 1, Column: 2}, results.Position{Line: 2, Column: 1}, true, 1, 3},
		{"yaml CRLF interior", source.YAML, "a\r\n", results.Position{Line: 1, Column: 3}, results.Position{Line: 2, Column: 1}, false, 0, 0},
		{"raw CR scalar", source.RawLF, "a\r\n", results.Position{Line: 1, Column: 2}, results.Position{Line: 1, Column: 3}, true, 1, 2},
		{"EOF point", source.YAML, "a\r\n", results.Position{Line: 2, Column: 1}, results.Position{Line: 2, Column: 1}, true, 3, 3},
		{"past EOF", source.YAML, "a\r\n", results.Position{Line: 2, Column: 1}, results.Position{Line: 2, Column: 2}, false, 0, 0},
		{"reversed", source.RawLF, "abc", results.Position{Line: 1, Column: 3}, results.Position{Line: 1, Column: 2}, false, 0, 0},
		{"zero end", source.YAML, "abc", results.Position{Line: 1, Column: 1}, results.Position{}, false, 0, 0},
		{"BOM Unicode", source.YAML, "\ufeff😀x", results.Position{Line: 1, Column: 1}, results.Position{Line: 1, Column: 2}, true, 3, 7},
		{"raw BOM scalar", source.RawLF, "\ufeff😀x", results.Position{Line: 1, Column: 1}, results.Position{Line: 1, Column: 2}, true, 0, 3},
		{"invalid UTF8", source.RawLF, string([]byte{0xff}), results.Position{Line: 1, Column: 1}, results.Position{Line: 1, Column: 1}, false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b, err := Range([]byte(tc.content), tc.model, tc.start, tc.end)
			if (err == nil) != tc.valid || tc.valid && (a != tc.a || b != tc.b) {
				t.Fatalf("range=(%d,%d) err=%v", a, b, err)
			}
		})
	}
}
