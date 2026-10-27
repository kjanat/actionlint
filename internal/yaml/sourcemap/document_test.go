package sourcemap

import (
	"testing"
	"unicode/utf8"
)

func TestDocumentColumnIndex(t *testing.T) {
	for _, text := range []string{"", "ascii", "é日本😀", "prefix日本suffix", "\t😀 x"} {
		d := NewDocument(text)
		column := 1
		for offset := range text {
			got, ok := d.ByteOffset(0, column)
			if !ok || got != offset {
				t.Fatalf("%q column %d = %d,%v; want %d", text, column, got, ok, offset)
			}
			if got := d.column(0, offset); got != column {
				t.Fatalf("%q offset %d = column %d; want %d", text, offset, got, column)
			}
			column++
		}
		end := utf8.RuneCountInString(text) + 1
		if got, ok := d.ByteOffset(0, end); !ok || got != len(text) {
			t.Fatalf("invalid end column for %q", text)
		}
		if d.column(0, len(text)) != end {
			t.Fatalf("invalid end offset for %q", text)
		}
		for _, pos := range [][2]int{{-1, 1}, {1, 1}, {0, 0}, {0, end + 1}} {
			if _, ok := d.ByteOffset(pos[0], pos[1]); ok {
				t.Fatalf("accepted invalid row/column %v in %q", pos, text)
			}
		}
	}
}

func TestDocumentPhysicalLines(t *testing.T) {
	for _, ending := range []string{"\n", "\r", "\r\n", "\u0085", "\u2028", "\u2029"} {
		d := NewDocument("\ufeffé" + ending + "日本")
		if len(d.Lines) != 2 || d.Lines[0] != "é" || d.Lines[1] != "日本" || d.Breaks[0] != ending || d.Breaks[1] != "" {
			t.Fatalf("incorrect physical lines: %+v", d)
		}
		if got, ok := d.ByteOffset(1, 2); !ok || got != len("日") {
			t.Fatalf("second-line offset = %d, %v", got, ok)
		}
		for index, want := range d.Lines {
			if got, ok := SourceLine("\ufeffé"+ending+"日本", index+1); !ok || got != want {
				t.Fatalf("source line %d = %q, %v; want %q", index+1, got, ok, want)
			}
		}
		for _, line := range []int{0, -1, 3} {
			if _, ok := SourceLine("\ufeffé"+ending+"日本", line); ok {
				t.Fatalf("accepted invalid line %d", line)
			}
		}
	}
}
