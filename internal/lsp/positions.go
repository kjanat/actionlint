// Package lsp serves editor requests using versioned buffers, findings and source edits.
package lsp

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/source"
	sourceposition "actionlint.kjanat.dev/v2/internal/input/source/position"
	"actionlint.kjanat.dev/v2/internal/results"
)

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type sourceRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

func editorPosition(content []byte, p results.Position) (position, error) {
	offset, err := sourceposition.Offset(content, source.YAML, p)
	if err != nil {
		return position{}, err
	}
	return positionAtByteOffset(content, offset), nil
}

func positionAtByteOffset(content []byte, offset int) position {
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
	return position{line, units}
}

func byteOffset(text string, p position) (int, error) {
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

func comparePosition(a, b position) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Character - b.Character
}

func diagnosticRange(content []byte, start, end results.Position) (sourceRange, error) {
	first, err := editorPosition(content, start)
	if err != nil {
		return sourceRange{}, err
	}
	if end == (results.Position{}) {
		return sourceRange{first, first}, nil
	}
	last, err := editorPosition(content, end)
	if err != nil {
		return sourceRange{}, err
	}
	if comparePosition(last, first) < 0 {
		last = first
	}
	return sourceRange{first, last}, nil
}
