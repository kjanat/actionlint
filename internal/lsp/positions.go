// Package lsp serves editor requests using versioned buffers, findings and source edits.
package lsp

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/byteoffset"
)

// editorPosition is a Language Server Protocol position. It holds a zero-based
// line and a zero-based column counted in UTF-16 code units, after any byte
// order mark. editorPosition differs from sourcetext.Position, which counts
// one-based lines and Unicode scalars, and the two convert only through a byte
// offset into the buffer.
type editorPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type editorRange struct {
	Start editorPosition `json:"start"`
	End   editorPosition `json:"end"`
}

func editorPositionAt(content []byte, p sourcetext.Position) (editorPosition, error) {
	offset, err := byteoffset.Offset(content, sourcetext.YAML, p)
	if err != nil {
		return editorPosition{}, err
	}
	return editorPositionAtByteOffset(content, offset), nil
}

func editorPositionAtByteOffset(content []byte, offset int) editorPosition {
	line, units := 0, 0
	start := 0
	if bytes.HasPrefix(content, []byte("\ufeff")) {
		start = len("\ufeff")
	}
	for i := start; i < offset; {
		r, n := utf8.DecodeRune(content[i:])
		if r == '\r' && i+n < len(content) && content[i+n] == '\n' {
			n++
		}
		if r == '\r' || r == '\n' {
			line++
			units = 0
		} else {
			units += utf16.RuneLen(r)
		}
		i += n
	}
	return editorPosition{line, units}
}

// editorByteOffset rejects positions inside a surrogate pair or outside the buffer.
func editorByteOffset(text string, p editorPosition) (int, error) {
	if p.Line < 0 || p.Character < 0 {
		return 0, errors.New("negative editor position")
	}
	line, units := 0, 0
	start := 0
	if strings.HasPrefix(text, "\ufeff") {
		start = len("\ufeff")
	}
	for offset := start; ; {
		if line == p.Line && units == p.Character {
			return offset, nil
		}
		if offset == len(text) {
			break
		}
		r, n := utf8.DecodeRuneInString(text[offset:])
		if r == '\r' && offset+n < len(text) && text[offset+n] == '\n' {
			n++
		}
		if r == '\n' || r == '\r' {
			line++
			units = 0
		} else {
			units += utf16.RuneLen(r)
		}
		offset += n
		if line > p.Line || line == p.Line && units > p.Character {
			return 0, errors.New("invalid UTF-16 position")
		}
	}
	return 0, errors.New("editor position outside buffer")
}

func compareEditorPositions(a, b editorPosition) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Character - b.Character
}

func diagnosticRange(content []byte, start, end sourcetext.Position) (editorRange, error) {
	first, err := editorPositionAt(content, start)
	if err != nil {
		return editorRange{}, err
	}
	if end == (sourcetext.Position{}) {
		return editorRange{first, first}, nil
	}
	last, err := editorPositionAt(content, end)
	if err != nil {
		return editorRange{}, err
	}
	if compareEditorPositions(last, first) < 0 {
		last = first
	}
	return editorRange{first, last}, nil
}
