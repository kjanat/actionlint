// Package byteoffset validates strict source ranges against captured bytes.
package byteoffset

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
)

// Offset resolves a physical Unicode-scalar position without inference or clamping.
func Offset(content []byte, model sourcetext.Coordinates, p sourcetext.Position) (int, error) {
	if p.Line < 1 || p.Column < 1 || !utf8.Valid(content) {
		return 0, fmt.Errorf("invalid source position %v", p)
	}
	switch model {
	case sourcetext.YAML:
		document := sourcemap.NewDocument(string(content))
		if p.Line > document.LineCount() {
			break
		}
		column, ok := document.ByteOffset(p.Line-1, p.Column)
		if !ok {
			break
		}
		offset := column
		if bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf}) {
			offset += 3
		}
		for i := range p.Line - 1 {
			text, ending := document.Line(i)
			offset += len(text) + len(ending)
		}
		return offset, nil
	case sourcetext.RawLF:
		start := 0
		for line := 1; ; line++ {
			end := len(content)
			if relative := bytes.IndexByte(content[start:], '\n'); relative >= 0 {
				end = start + relative
			}
			if line == p.Line {
				if column, ok := sourcetext.ByteOffsetAtColumn(string(content[start:end]), p.Column); ok {
					return start + column, nil
				}
				break
			}
			if end == len(content) {
				break
			}
			start = end + 1
		}
	default:
		return 0, fmt.Errorf("invalid source coordinate model %q", model)
	}
	return 0, fmt.Errorf("source position %v is outside the document", p)
}

// RawLFPosition returns the sourcetext.RawLF position of a byte offset in content,
// clamping the offset to the content. It is the inverse of Offset for RawLF:
// lines end at LF only, and columns count Unicode scalars.
func RawLFPosition(content []byte, offset int) sourcetext.Position {
	offset = max(0, min(offset, len(content)))
	prefix := content[:offset]
	return sourcetext.Position{
		Line:   1 + bytes.Count(prefix, []byte{'\n'}),
		Column: 1 + utf8.RuneCount(prefix[bytes.LastIndexByte(prefix, '\n')+1:]),
	}
}

// Range validates both endpoints and order; zero-width ranges, including EOF,
// are valid. Zero endpoints, reversed ranges and YAML CRLF interiors are not.
func Range(content []byte, model sourcetext.Coordinates, start, end sourcetext.Position) (int, int, error) {
	a, err := Offset(content, model, start)
	if err != nil {
		return 0, 0, err
	}
	b, err := Offset(content, model, end)
	if err != nil {
		return 0, 0, err
	}
	if b < a {
		return 0, 0, errors.New("source range end precedes start")
	}
	return a, b, nil
}
