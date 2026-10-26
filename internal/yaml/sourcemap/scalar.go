// Package sourcemap decodes YAML scalar tokens with their source provenance.
package sourcemap

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Position uses one-based Unicode character columns and physical source lines.
type Position struct{ Line, Column int }

// Span associates decoded bytes with one source token. Width is nonzero for an
// escape; copied text has the same rune width in the source and decoded value.
type Span struct {
	Start, End int
	Position
	Width     int
	LineBreak bool
}

// Scalar is the result of a single token scan. Folding and line normalization
// generate whitespace without a source token, so those bytes have no Span.
type Scalar struct {
	Value      string
	Spans      []Span
	Start, End Position
	Literal    bool
}

// Context is supplied by the enclosing YAML collection, independently of the
// scalar's decoded value. Indent is zero-based; root scalars use -1.
type Context struct {
	Indent int
	Flow   bool
}

type decoder struct {
	document                                  *Document
	lines                                     []string
	row, col                                  int
	value                                     strings.Builder
	result                                    Scalar
	context                                   Context
	positionRow, positionByte, positionColumn int
	lastColumn                                int
}

// Decode scans a scalar at start, including any tag and anchor properties.
// The caller validates Value against its YAML parser before publishing mappings.
func decode(document *Document, start Position, context Context) (Scalar, error) {
	lines := document.Lines
	d := decoder{document: document, lines: lines, row: start.Line - 1, context: context}
	if d.row < 0 || d.row >= len(lines) || start.Column < 1 {
		return Scalar{}, errors.New("invalid scalar position")
	}
	var ok bool
	d.col, ok = document.ByteOffset(d.row, start.Column)
	if !ok {
		return Scalar{}, errors.New("invalid scalar column")
	}
	if end, empty := d.properties(); empty {
		return Scalar{Start: end, End: end}, nil
	}
	d.result.Start = d.position()
	var err error
	switch d.peek() {
	case '|', '>':
		d.block()
	case '\'', '"':
		err = d.flow(d.peek())
	default:
		err = d.flow(0)
	}
	if err != nil {
		return Scalar{}, err
	}
	d.result.Value = d.value.String()
	return d.result, nil
}

func (d *decoder) peek() byte {
	if d.row >= len(d.lines) || d.col >= len(d.lines[d.row]) {
		return 0
	}
	return d.lines[d.row][d.col]
}

func (d *decoder) position() Position {
	if d.row >= len(d.lines) {
		return Position{len(d.lines), utf8.RuneCountInString(d.lines[len(d.lines)-1]) + 1}
	}
	if d.positionColumn == 0 || d.positionRow != d.row || d.positionByte > d.col {
		d.positionRow, d.positionByte, d.positionColumn = d.row, d.col, d.document.column(d.row, d.col)
	}
	d.positionColumn += utf8.RuneCountInString(d.lines[d.row][d.positionByte:d.col])
	d.positionByte = d.col
	return Position{d.row + 1, d.positionColumn}
}

func (d *decoder) properties() (Position, bool) {
	var tag, anchor bool
	end := d.position()
	for d.peek() == '!' || d.peek() == '&' {
		isAnchor := d.peek() == '&'
		// A scalar has at most one tag and one anchor. Unmarshal can return an
		// empty first scalar before a subsequent token starts another document.
		if d.peek() == '!' {
			if tag {
				return end, true
			}
			tag = true
		} else {
			if anchor {
				return end, true
			}
			anchor = true
		}
		verbatim := d.peek() == '!' && strings.HasPrefix(d.lines[d.row][d.col:], "!<")
		for d.peek() != 0 {
			c := d.peek()
			if !verbatim && (strings.ContainsRune(" \t,[]{}", rune(c)) || isAnchor && (c == ':' || c >= utf8.RuneSelf)) {
				break
			}
			d.col++
			if verbatim && c == '>' {
				break
			}
		}
		end = d.position()
		for d.row < len(d.lines) {
			for d.peek() == ' ' || d.peek() == '\t' {
				d.col++
			}
			if d.peek() != 0 && d.peek() != '#' {
				if d.row+1 > end.Line && !d.context.Flow && d.col <= d.context.Indent {
					return end, true
				}
				break
			}
			d.row++
			d.col = 0
		}
		if d.row == len(d.lines) {
			return end, true
		}
	}
	if tag || anchor {
		// A following alias or collection indicator ends an empty scalar
		// after its properties.
		c := d.peek()
		if strings.ContainsRune("*,[]{}", rune(c)) || d.plainEnd() {
			return end, true
		}
		if c == '-' || c == '?' {
			next := d.col + 1
			if next == len(d.lines[d.row]) || strings.ContainsRune(" \t", rune(d.lines[d.row][next])) {
				return end, true
			}
		}
	}
	return Position{}, false
}

// emit is the sole writer of decoded bytes and their corresponding provenance.
func (d *decoder) emit(value string, pos Position, width int) {
	start := d.value.Len()
	d.value.WriteString(value)
	if value == "" || pos.Line == 0 {
		return
	}
	span := Span{Start: start, End: d.value.Len(), Position: pos, Width: width}
	if n := len(d.result.Spans); n > 0 && width == 0 {
		last := &d.result.Spans[n-1]
		if last.Width == 0 && last.End == start && last.Line == pos.Line && d.lastColumn == pos.Column {
			last.End = span.End
			d.lastColumn = pos.Column + utf8.RuneCountInString(value)
			return
		}
	}
	d.result.Spans = append(d.result.Spans, span)
	d.lastColumn = pos.Column + utf8.RuneCountInString(value)
}

func (d *decoder) copyRune() {
	_, size := utf8.DecodeRuneInString(d.lines[d.row][d.col:])
	d.emit(d.lines[d.row][d.col:d.col+size], d.position(), 0)
	d.col += size
}

// flow handles both quoted and plain scalars with the same whitespace folding
// state. Quotes only change termination and escape processing.
func (d *decoder) flow(quote byte) error {
	if quote != 0 {
		d.col++
	}
	d.result.End = d.position()
	for d.row < len(d.lines) {
		c := d.peek()
		if quote != 0 && c == quote {
			if quote == '\'' && strings.HasPrefix(d.lines[d.row][d.col:], "''") {
				d.emit("'", d.position(), 2)
				d.col += 2
				continue
			}
			d.col++
			d.result.End = d.position()
			return nil
		}
		if quote == 0 && d.plainEnd() {
			return nil
		}
		if quote == '"' && c == '\\' {
			if d.col+1 == len(d.lines[d.row]) {
				d.col++
				if !d.whitespace(quote, true) {
					return errors.New("unterminated quoted scalar")
				}
				continue
			}
			if err := d.escape(); err != nil {
				return err
			}
			continue
		}
		if c == 0 || c == ' ' || c == '\t' {
			if !d.whitespace(quote, false) {
				if quote != 0 {
					return errors.New("unterminated quoted scalar")
				}
				return nil
			}
			continue
		}
		d.copyRune()
		d.result.End = d.position()
	}
	if quote != 0 {
		return errors.New("unterminated quoted scalar")
	}
	return nil
}

func (d *decoder) plainEnd() bool {
	c := d.peek()
	comment := c == '#' && (d.col == 0 || strings.ContainsRune(" \t", rune(d.lines[d.row][d.col-1])))
	if comment || d.context.Flow && strings.ContainsRune(",[]{}", rune(c)) {
		return true
	}
	if c == ':' {
		next := d.col + 1
		if next == len(d.lines[d.row]) || strings.ContainsRune(" \t", rune(d.lines[d.row][next])) || d.context.Flow && strings.ContainsRune(",]}", rune(d.lines[d.row][next])) {
			return true
		}
	}
	if c == '?' && d.context.Flow {
		next := d.col + 1
		if next == len(d.lines[d.row]) || strings.ContainsRune(" \t", rune(d.lines[d.row][next])) {
			return true
		}
	}
	return d.col == 0 && (d.documentMarker("---") || d.documentMarker("..."))
}

func (d *decoder) documentMarker(marker string) bool {
	s := d.lines[d.row]
	return strings.HasPrefix(s, marker) && (len(s) == 3 || s[3] == ' ' || s[3] == '\t')
}

func (d *decoder) whitespace(quote byte, continued bool) bool {
	start, pos := d.col, d.position()
	for d.peek() == ' ' || d.peek() == '\t' {
		d.col++
	}
	if d.peek() != 0 {
		if quote == 0 && d.plainEnd() {
			return false
		}
		d.emit(d.lines[d.row][start:d.col], pos, 0)
		d.result.End = d.position()
		return true
	}
	var breaks []int
	for d.row+1 < len(d.lines) {
		breaks = append(breaks, d.row)
		d.row++
		d.col = 0
		for d.peek() == ' ' || d.peek() == '\t' {
			d.col++
		}
		if d.peek() != 0 {
			break
		}
	}
	if d.peek() == 0 {
		return false
	}
	if quote == 0 && (!d.context.Flow && d.col <= d.context.Indent || d.plainEnd()) {
		return false
	}
	if continued && len(breaks) > 0 {
		d.emitBreaks(breaks[1:], false)
	} else {
		d.emitBreaks(breaks, true)
	}
	return true
}

// YAML normalizes physical line breaks to LF except for LS and PS.
func (d *decoder) emitBreaks(rows []int, fold bool) {
	for i, row := range rows {
		value := d.document.Breaks[row]
		switch {
		case value == "\u2028" || value == "\u2029":
			d.emit(value, Position{row + 1, utf8.RuneCountInString(d.lines[row]) + 1}, 1)
			d.result.Spans[len(d.result.Spans)-1].LineBreak = true
		case i != 0 || !fold:
			d.emit("\n", Position{}, 0)
		case len(rows) == 1:
			d.emit(" ", Position{}, 0)
		}
	}
}

func (d *decoder) escape() error {
	start := d.col
	pos := d.position()
	d.col++
	c := d.peek()
	d.col++
	escapes := map[byte]string{'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n", 'v': "\v", 'f': "\f", 'r': "\r", 'e': "\x1b", ' ': " ", '"': "\"", '\'': "'", '\\': "\\", 'N': "\u0085", '_': "\u00a0", 'L': "\u2028", 'P': "\u2029"}
	value, ok := escapes[c]
	if !ok {
		width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
		if width == 0 || d.col+width > len(d.lines[d.row]) {
			return errors.New("invalid scalar escape")
		}
		r, err := strconv.ParseUint(d.lines[d.row][d.col:d.col+width], 16, 32)
		if err != nil || r > utf8.MaxRune {
			return errors.New("invalid scalar escape")
		}
		if !utf8.ValidRune(rune(r)) {
			return errors.New("invalid scalar escape")
		}
		value = string(rune(r))
		d.col += width
	}
	d.emit(value, pos, d.col-start)
	d.result.End = d.position()
	return nil
}

func (d *decoder) block() {
	literal := d.peek() == '|'
	d.result.Literal = literal
	d.col++
	chomp, increment := byte(0), 0
	for c := d.peek(); c == '+' || c == '-' || c >= '1' && c <= '9'; c = d.peek() {
		if c == '+' || c == '-' {
			chomp = c
		} else {
			increment = int(c - '0')
		}
		d.col++
	}
	d.result.End = d.position()
	d.row++
	d.col = 0
	indent := max(d.context.Indent, 0) + increment
	if increment == 0 {
		indent = 0
	}
	var pending []int
	previous, hadContent := false, false
	for d.row < len(d.lines) {
		raw := d.lines[d.row]
		spaces := len(raw) - len(strings.TrimLeft(raw, " "))
		if increment == 0 && !hadContent {
			indent = max(indent, spaces, d.context.Indent+1)
		}
		if spaces == len(raw) && (spaces <= indent || !hadContent && increment == 0) {
			if d.row+1 < len(d.lines) {
				pending = append(pending, d.row)
				d.result.End = Position{d.row + 1, len(raw) + 1}
			}
			d.row++
			continue
		}
		if spaces < indent {
			break
		}
		d.col = indent
		blank := d.peek() == ' ' || d.peek() == '\t'
		d.emitBreaks(pending, hadContent && !literal && !previous && !blank)
		pending = pending[:0]
		for d.peek() != 0 {
			d.copyRune()
		}
		d.result.End = d.position()
		hadContent = true
		previous = blank
		if d.row+1 < len(d.lines) {
			pending = append(pending, d.row)
		}
		d.row++
		d.col = 0
	}
	if chomp == '+' {
		d.emitBreaks(pending, false)
	} else if chomp != '-' && hadContent && len(pending) > 0 {
		d.emitBreaks(pending[:1], false)
	}
}
