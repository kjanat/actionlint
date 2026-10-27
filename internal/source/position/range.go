// Package position validates strict source ranges against captured bytes.
package position

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/source/text"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/source"
)

// Offset resolves a physical Unicode-scalar position without inference or clamping.
func Offset(content []byte, model source.Coordinates, p diagnostic.Position) (int, error) {
	if p.Line < 1 || p.Column < 1 || !utf8.Valid(content) {
		return 0, fmt.Errorf("invalid source position %v", p)
	}
	switch model {
	case source.YAML:
		document := sourcemap.NewDocument(string(content))
		if p.Line > len(document.Lines) {
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
			offset += len(document.Lines[i]) + len(document.Breaks[i])
		}
		return offset, nil
	case source.RawLF:
		start := 0
		for line := 1; ; line++ {
			end := len(content)
			if relative := bytes.IndexByte(content[start:], '\n'); relative >= 0 {
				end = start + relative
			}
			if line == p.Line {
				if column, ok := text.ByteOffsetAtColumn(string(content[start:end]), p.Column); ok {
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

// Range validates both endpoints and order; zero-width ranges, including EOF,
// are valid. Zero endpoints, reversed ranges and YAML CRLF interiors are not.
func Range(content []byte, model source.Coordinates, start, end diagnostic.Position) (int, int, error) {
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
