package position

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
)

func TestRawLFPositionInvertsOffset(t *testing.T) {
	t.Parallel()
	content := []byte("a\r\nβc\nd")
	for offset := range len(content) + 1 {
		if offset == 4 {
			continue // inside the two-byte β
		}
		p := RawLFPosition(content, offset)
		back, err := Offset(content, source.RawLF, p)
		if err != nil || back != offset {
			t.Fatalf("offset %d -> %v -> %d, %v", offset, p, back, err)
		}
	}
	if got := RawLFPosition(content, -1); got != (source.Position{Line: 1, Column: 1}) {
		t.Fatalf("negative offset = %v", got)
	}
	if got := RawLFPosition(content, 99); got != (source.Position{Line: 3, Column: 2}) {
		t.Fatalf("offset past end = %v", got)
	}
}

func TestStrictCapturedRanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		model      source.Coordinates
		content    string
		start, end source.Position
		valid      bool
		a, b       int
	}{
		{"yaml CRLF", source.YAML, "a\r\n", source.Position{Line: 1, Column: 2}, source.Position{Line: 2, Column: 1}, true, 1, 3},
		{"yaml CRLF interior", source.YAML, "a\r\n", source.Position{Line: 1, Column: 3}, source.Position{Line: 2, Column: 1}, false, 0, 0},
		{"raw CR scalar", source.RawLF, "a\r\n", source.Position{Line: 1, Column: 2}, source.Position{Line: 1, Column: 3}, true, 1, 2},
		{"EOF point", source.YAML, "a\r\n", source.Position{Line: 2, Column: 1}, source.Position{Line: 2, Column: 1}, true, 3, 3},
		{"past EOF", source.YAML, "a\r\n", source.Position{Line: 2, Column: 1}, source.Position{Line: 2, Column: 2}, false, 0, 0},
		{"reversed", source.RawLF, "abc", source.Position{Line: 1, Column: 3}, source.Position{Line: 1, Column: 2}, false, 0, 0},
		{"zero end", source.YAML, "abc", source.Position{Line: 1, Column: 1}, source.Position{}, false, 0, 0},
		{"BOM Unicode", source.YAML, "\ufeff😀x", source.Position{Line: 1, Column: 1}, source.Position{Line: 1, Column: 2}, true, 3, 7},
		{"raw BOM scalar", source.RawLF, "\ufeff😀x", source.Position{Line: 1, Column: 1}, source.Position{Line: 1, Column: 2}, true, 0, 3},
		{"invalid UTF8", source.RawLF, string([]byte{0xff}), source.Position{Line: 1, Column: 1}, source.Position{Line: 1, Column: 1}, false, 0, 0},
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

func TestYAMLPhysicalCoordinates(t *testing.T) {
	t.Parallel()
	for _, separator := range []string{"\r", "\r\n", "\n", "\u0085", "\u2028", "\u2029"} {
		content := []byte("\ufeffα" + separator + "β")
		offset, err := Offset(content, source.YAML, source.Position{Line: 2, Column: 1})
		if err != nil || string(content[offset:]) != "β" {
			t.Fatalf("separator=%q offset=%d err=%v", separator, offset, err)
		}
	}
}
