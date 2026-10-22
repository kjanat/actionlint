package langserver

import (
	"testing"

	"actionlint.kjanat.dev/diagnostic"
)

func TestBOMEditorCoordinatesRoundTrip(t *testing.T) {
	text := "\ufeffa😀b\r\néx"
	for _, point := range []struct {
		editor position
		source diagnostic.Position
		offset int
	}{
		{position{0, 0}, diagnostic.Position{Line: 1, Column: 1}, 3},
		{position{0, 1}, diagnostic.Position{Line: 1, Column: 2}, 4},
		{position{0, 3}, diagnostic.Position{Line: 1, Column: 3}, 8},
		{position{0, 4}, diagnostic.Position{Line: 1, Column: 4}, 9},
		{position{1, 0}, diagnostic.Position{Line: 2, Column: 1}, 11},
		{position{1, 1}, diagnostic.Position{Line: 2, Column: 2}, 13},
		{position{1, 2}, diagnostic.Position{Line: 2, Column: 3}, 14},
	} {
		if offset, err := byteOffset(text, point.editor); err != nil || offset != point.offset {
			t.Errorf("editor %v: offset=%d err=%v, want %d", point.editor, offset, err, point.offset)
		}
		if got := editorPosition([]byte(text), point.source); got != point.editor {
			t.Errorf("source %v: editor=%v, want %v", point.source, got, point.editor)
		}
		if got := positionAtOffset(text, point.offset); got != point.editor {
			t.Errorf("completion byte %d: editor=%v, want %v", point.offset, got, point.editor)
		}
	}
	if _, err := byteOffset(text, position{0, 2}); err == nil {
		t.Error("accepted an editor position inside a surrogate pair")
	}
}

func TestBOMRangedChangePreservesMarker(t *testing.T) {
	text := "\ufeffabc"
	start, err := byteOffset(text, position{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	end, err := byteOffset(text, position{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if changed := text[:start] + "z" + text[end:]; changed != "\ufeffzbc" {
		t.Fatalf("first visible character replacement corrupted the BOM: %q", changed)
	}
	if offset, err := byteOffset("a\ufeffb", position{0, 2}); err != nil || offset != 4 {
		t.Fatalf("a noninitial U+FEFF must remain visible: offset=%d err=%v", offset, err)
	}
	if offset, err := byteOffset("\ufeff", position{0, 0}); err != nil || offset != 3 {
		t.Fatalf("BOM-only empty buffer: offset=%d err=%v", offset, err)
	}
}
