package sourcemap

import (
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

// Span maps the decoded bytes [Start, End) of a scalar value to the YAML
// source. Document is the source position of the first mapped character.
// DocumentEnd is zero for text copied from the source, whose decoded and source
// widths match. A decoded escape or line separator sets DocumentEnd to the
// exclusive source end of that token.
type Span struct {
	Start, End  int
	Document    sourcetext.Position
	DocumentEnd sourcetext.Position
}

func (s Span) end(value string) sourcetext.Position {
	if s.DocumentEnd.Line != 0 {
		return s.DocumentEnd
	}
	return sourcetext.Position{Line: s.Document.Line, Column: s.Document.Column + utf8.RuneCountInString(value[s.Start:s.End])}
}

// at is the source position of offset in [Start, End]. A decoded escape maps
// every interior offset to its start and only an exclusive End to its end.
func (s Span) at(value string, offset int, exclusive bool) sourcetext.Position {
	if s.DocumentEnd.Line == 0 {
		return sourcetext.Position{Line: s.Document.Line, Column: s.Document.Column + utf8.RuneCountInString(value[s.Start:offset])}
	}
	if exclusive && offset == s.End {
		return s.DocumentEnd
	}
	return s.Document
}

// Locate maps a byte offset of the decoded value to the YAML source. spans
// must be sorted by End, as Decode returns them.
func Locate(value string, spans []Span, offset int, exclusive bool) (sourcetext.Position, bool) {
	if exclusive && offset == len(value) && len(spans) != 0 {
		index := len(spans) - 1
		for index > 0 && spans[index].Start == spans[index].End {
			index--
		}
		last := spans[index]
		if last.End < offset && strings.Trim(value[last.End:], " \n") == "" {
			return last.end(value), true
		}
	}
	i := spanIndex(spans, offset, exclusive)
	if i == len(spans) {
		return sourcetext.Position{}, false
	}
	span := spans[i]
	if offset < span.Start || (exclusive && offset == span.Start) {
		return sourcetext.Position{}, false
	}
	return span.at(value, offset, exclusive), true
}

func spanIndex(spans []Span, offset int, exclusive bool) int {
	low, high := 0, len(spans)
	for low < high {
		mid := int(uint(low+high) >> 1)
		end := spans[mid].End
		if end > offset || (exclusive && end == offset) {
			high = mid
		} else {
			low = mid + 1
		}
	}
	return low
}

// LineSpans splits spans at the line feeds of value, and each result lies on
// one text line. See the span record in plugins/protocol/wit/analyzer.wit.
func LineSpans(value string, spans []Span) []Span {
	var result []Span
	var last *Span
	lineStart := 0
	for {
		lineEnd := len(value)
		if i := strings.IndexByte(value[lineStart:], '\n'); i >= 0 {
			lineEnd = lineStart + i
		}
		limit := lineEnd
		if limit < len(value) {
			limit++
		}
		for j := range spans {
			span := &spans[j]
			start, end := max(span.Start, lineStart), min(span.End, limit)
			if span.Start >= span.End || start >= end {
				continue
			}
			last = span
			result = append(result, Span{Start: start, End: end, Document: span.at(value, start, false), DocumentEnd: span.DocumentEnd})
			if end == limit && limit > lineEnd {
				next := span.DocumentEnd
				if next.Line == 0 {
					next = span.at(value, end, true)
				}
				result = append(result, Span{Start: limit, End: limit, Document: next})
			}
		}
		if lineEnd == len(value) {
			break
		}
		lineStart = lineEnd + 1
	}
	if last != nil && last.End < len(value) && strings.Trim(value[last.End:], " \n") == "" {
		result = append(result, Span{Start: len(value), End: len(value), Document: last.end(value)})
	}
	return result
}
