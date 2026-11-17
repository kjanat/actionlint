package byteoffset

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

func TestRawLFPositionInvertsOffset(t *testing.T) {
	t.Parallel()
	content := []byte("a\r\nβc\nd")
	for offset := range len(content) + 1 {
		if offset == 4 {
			continue // inside the two-byte β
		}
		p := RawLFPosition(content, offset)
		back, err := Offset(content, sourcetext.RawLF, p)
		if err != nil || back != offset {
			t.Fatalf("offset %d -> %v -> %d, %v", offset, p, back, err)
		}
	}
	if got := RawLFPosition(content, -1); got != (sourcetext.Position{Line: 1, Column: 1}) {
		t.Fatalf("negative offset = %v", got)
	}
	if got := RawLFPosition(content, 99); got != (sourcetext.Position{Line: 3, Column: 2}) {
		t.Fatalf("offset past end = %v", got)
	}
}

func TestStrictCapturedRanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		model      sourcetext.Coordinates
		content    string
		start, end sourcetext.Position
		valid      bool
		a, b       int
	}{
		{"yaml CRLF", sourcetext.YAML, "a\r\n", sourcetext.Position{Line: 1, Column: 2}, sourcetext.Position{Line: 2, Column: 1}, true, 1, 3},
		{"yaml CRLF interior", sourcetext.YAML, "a\r\n", sourcetext.Position{Line: 1, Column: 3}, sourcetext.Position{Line: 2, Column: 1}, false, 0, 0},
		{"raw CR scalar", sourcetext.RawLF, "a\r\n", sourcetext.Position{Line: 1, Column: 2}, sourcetext.Position{Line: 1, Column: 3}, true, 1, 2},
		{"EOF point", sourcetext.YAML, "a\r\n", sourcetext.Position{Line: 2, Column: 1}, sourcetext.Position{Line: 2, Column: 1}, true, 3, 3},
		{"past EOF", sourcetext.YAML, "a\r\n", sourcetext.Position{Line: 2, Column: 1}, sourcetext.Position{Line: 2, Column: 2}, false, 0, 0},
		{"reversed", sourcetext.RawLF, "abc", sourcetext.Position{Line: 1, Column: 3}, sourcetext.Position{Line: 1, Column: 2}, false, 0, 0},
		{"zero end", sourcetext.YAML, "abc", sourcetext.Position{Line: 1, Column: 1}, sourcetext.Position{}, false, 0, 0},
		{"BOM Unicode", sourcetext.YAML, "\ufeff😀x", sourcetext.Position{Line: 1, Column: 1}, sourcetext.Position{Line: 1, Column: 2}, true, 3, 7},
		{"raw BOM scalar", sourcetext.RawLF, "\ufeff😀x", sourcetext.Position{Line: 1, Column: 1}, sourcetext.Position{Line: 1, Column: 2}, true, 0, 3},
		{"invalid UTF8", sourcetext.RawLF, string([]byte{0xff}), sourcetext.Position{Line: 1, Column: 1}, sourcetext.Position{Line: 1, Column: 1}, false, 0, 0},
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
		offset, err := Offset(content, sourcetext.YAML, sourcetext.Position{Line: 2, Column: 1})
		if err != nil || string(content[offset:]) != "β" {
			t.Fatalf("separator=%q offset=%d err=%v", separator, offset, err)
		}
	}
}
