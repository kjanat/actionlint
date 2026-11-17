package lsp

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

func TestBOMEditorCoordinatesRoundTrip(t *testing.T) {
	t.Parallel()
	text := "\ufeffa😀b\r\néx"
	for _, point := range []struct {
		editor editorPosition
		source sourcetext.Position
		offset int
	}{
		{editorPosition{0, 0}, sourcetext.Position{Line: 1, Column: 1}, 3},
		{editorPosition{0, 1}, sourcetext.Position{Line: 1, Column: 2}, 4},
		{editorPosition{0, 3}, sourcetext.Position{Line: 1, Column: 3}, 8},
		{editorPosition{0, 4}, sourcetext.Position{Line: 1, Column: 4}, 9},
		{editorPosition{1, 0}, sourcetext.Position{Line: 2, Column: 1}, 11},
		{editorPosition{1, 1}, sourcetext.Position{Line: 2, Column: 2}, 13},
		{editorPosition{1, 2}, sourcetext.Position{Line: 2, Column: 3}, 14},
	} {
		if offset, err := editorByteOffset(text, point.editor); err != nil || offset != point.offset {
			t.Errorf("editor %v: offset=%d err=%v, want %d", point.editor, offset, err, point.offset)
		}
		if got, err := editorPositionAt([]byte(text), point.source); err != nil || got != point.editor {
			t.Errorf("source %v: editor=%v error=%v, want %v", point.source, got, err, point.editor)
		}
		if got := editorPositionAtByteOffset([]byte(text), point.offset); got != point.editor {
			t.Errorf("completion byte %d: editor=%v, want %v", point.offset, got, point.editor)
		}
	}
	if _, err := editorByteOffset(text, editorPosition{0, 2}); err == nil {
		t.Error("accepted an editor position inside a surrogate pair")
	}
}

func TestBOMRangedChangePreservesMarker(t *testing.T) {
	t.Parallel()
	text := "\ufeffabc"
	start, err := editorByteOffset(text, editorPosition{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	end, err := editorByteOffset(text, editorPosition{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if changed := text[:start] + "z" + text[end:]; changed != "\ufeffzbc" {
		t.Fatalf("first visible character replacement corrupted the BOM: %q", changed)
	}
	if offset, err := editorByteOffset("a\ufeffb", editorPosition{0, 2}); err != nil || offset != 4 {
		t.Fatalf("a noninitial U+FEFF must remain visible: offset=%d err=%v", offset, err)
	}
	if offset, err := editorByteOffset("\ufeff", editorPosition{0, 0}); err != nil || offset != 3 {
		t.Fatalf("BOM-only empty buffer: offset=%d err=%v", offset, err)
	}
}
