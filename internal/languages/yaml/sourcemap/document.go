package sourcemap

import (
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Document indexes the physical lines and original line endings once. YAML
// recognizes CR, LF, CRLF, NEL, LS, and PS as physical line boundaries.
type Document struct {
	lines   []string
	breaks  []string
	columns [][]int
}

func NewDocument(source string) *Document {
	d := &Document{}
	visitLines(source, func(line, ending string) bool {
		d.addLine(line, ending)
		return true
	})
	return d
}

// SourceLine returns a physical YAML line using the decoder's line model.
// It stops scanning after the requested one-based line.
func SourceLine(source string, line int) (string, bool) {
	if line < 1 {
		return "", false
	}
	var result string
	found := false
	visitLines(source, func(text, _ string) bool {
		line--
		if line == 0 {
			result, found = text, true
			return false
		}
		return true
	})
	return result, found
}

func visitLines(source string, visit func(string, string) bool) {
	// The YAML reader consumes a stream BOM before counting columns.
	source = strings.TrimPrefix(source, "\ufeff")
	start := 0
	for offset, r := range source {
		if offset < start {
			continue
		}
		size := 0
		switch r {
		case '\n':
			size = 1
		case '\r':
			size = 1
			if offset+1 < len(source) && source[offset+1] == '\n' {
				size = 2
			}
		case '\u0085':
			size = 2
		case '\u2028', '\u2029':
			size = 3
		}
		if size == 0 {
			continue
		}
		if !visit(source[start:offset], source[offset:offset+size]) {
			return
		}
		start = offset + size
	}
	visit(source[start:], "")
}

func (d *Document) addLine(text, ending string) {
	d.lines = append(d.lines, text)
	d.breaks = append(d.breaks, ending)
	var offsets []int
	if utf8.RuneCountInString(text) != len(text) {
		for offset := range text {
			offsets = append(offsets, offset)
		}
		offsets = append(offsets, len(text))
	}
	d.columns = append(d.columns, offsets)
}

// LineCount returns the number of physical lines, including a final empty line.
func (d *Document) LineCount() int { return len(d.lines) }

// Line returns a zero-based physical line and the break that ended it. The last
// line has no break. Like slice indexing, row must be within LineCount.
func (d *Document) Line(row int) (text, ending string) { return d.lines[row], d.breaks[row] }

// Lines returns a copy of the physical lines without their breaks.
func (d *Document) Lines() []string { return slices.Clone(d.lines) }

// ByteOffset resolves a one-based column without rescanning a long flow line.
func (d *Document) ByteOffset(row, column int) (int, bool) {
	if row < 0 || row >= len(d.lines) || column < 1 {
		return 0, false
	}
	if offsets := d.columns[row]; offsets != nil {
		if column > len(offsets) {
			return 0, false
		}
		return offsets[column-1], true
	}
	return column - 1, column-1 <= len(d.lines[row])
}

func (d *Document) column(row, offset int) int {
	if offsets := d.columns[row]; offsets != nil {
		return sort.SearchInts(offsets, offset) + 1
	}
	return offset + 1
}

func (d *Document) Decode(start Position, context Context) (Scalar, error) {
	return decode(d, start, context)
}
