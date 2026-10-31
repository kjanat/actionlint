package process

import (
	"unicode/utf16"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// coordinates converts one tool's positions in one snippet to the
// analyzer.ScriptPosition contract: LF rows, Unicode scalar columns and
// exclusive ends. The zero value converts nothing.
type coordinates struct {
	convention provider.Coordinates
	lines      []engineLine
}

// engineLine is one line as the tool counts lines, and the contract position
// of its first character.
type engineLine struct {
	text  string
	start analyzer.ScriptPosition
}

// defaultCoordinates is the contract's own convention.
var defaultCoordinates = provider.Coordinates{Rows: provider.RowsLF, Columns: provider.ColumnsChars, End: provider.EndExclusive}

func newCoordinates(source string, convention *provider.Coordinates) coordinates {
	if convention == nil || *convention == defaultCoordinates {
		return coordinates{}
	}
	c := coordinates{convention: *convention}
	position := analyzer.ScriptPosition{Row: 1, Column: 1}
	line := engineLine{start: position}
	begin := 0
	for i, character := range source {
		universal := convention.Rows == provider.RowsSource && character == '\r' && (i+1 == len(source) || source[i+1] != '\n')
		switch {
		case character == '\n':
			end := i
			if convention.Rows == provider.RowsSource && end > begin && source[end-1] == '\r' {
				end--
			}
			line.text = source[begin:end]
			c.lines = append(c.lines, line)
			position = analyzer.ScriptPosition{Row: position.Row + 1, Column: 1}
			line, begin = engineLine{start: position}, i+1
		case universal:
			line.text = source[begin:i]
			c.lines = append(c.lines, line)
			position.Column++
			line, begin = engineLine{start: position}, i+1
		default:
			position.Column++
		}
	}
	line.text = source[begin:]
	c.lines = append(c.lines, line)
	return c
}

// start converts a start position.
func (c coordinates) start(position analyzer.ScriptPosition) analyzer.ScriptPosition {
	return c.convert(position, false)
}

// end converts an end position; the zero position means no end.
func (c coordinates) end(position analyzer.ScriptPosition) analyzer.ScriptPosition {
	if position == (analyzer.ScriptPosition{}) {
		return position
	}
	return c.convert(position, c.convention.End == provider.EndInclusive)
}

func (c coordinates) convert(position analyzer.ScriptPosition, inclusive bool) analyzer.ScriptPosition {
	if c.lines == nil || position.Row < 1 || position.Row > len(c.lines) || position.Column < 1 {
		return position
	}
	line := c.lines[position.Row-1]
	units, characters := position.Column-1, 0
	text := line.text
	for units > 0 && text != "" {
		character, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		switch c.convention.Columns {
		case provider.ColumnsBytes:
			units -= size
		case provider.ColumnsUTF16:
			units -= utf16.RuneLen(character)
		case provider.ColumnsChars:
			units--
		}
		characters++
	}
	// Positions past the line's end keep their remaining distance.
	characters += max(units, 0)
	converted := analyzer.ScriptPosition{Row: line.start.Row, Column: line.start.Column + characters}
	if inclusive {
		converted.Column++
	}
	return converted
}

// diagnostic converts a snippet diagnostic and its fix replacements in place.
func (c coordinates) diagnostic(d *analyzer.ScriptDiagnostic) {
	if c.lines == nil {
		return
	}
	d.Location = c.start(d.Location)
	d.EndLocation = c.end(d.EndLocation)
	for i := range d.Fixes {
		for j := range d.Fixes[i].Replacements {
			r := &d.Fixes[i].Replacements[j]
			start := c.start(analyzer.ScriptPosition{Row: r.Row, Column: r.Column})
			end := c.end(analyzer.ScriptPosition{Row: r.EndRow, Column: r.EndColumn})
			r.Row, r.Column, r.EndRow, r.EndColumn = start.Row, start.Column, end.Row, end.Column
		}
	}
}
