// Package enginediagnostics admits the diagnostics a script engine returns
// for one snippet. Every transport, process tools and WebAssembly engines
// alike, validates and converts engine positions here, so all of them accept
// the same ranges and produce the same scriptengine.Diagnostic coordinates.
package enginediagnostics

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

// ContractCoordinates is the convention of scriptengine.Diagnostic itself:
// LF rows, Unicode scalar columns and exclusive ends.
var ContractCoordinates = toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndExclusive}

// UniversalNewlineCoordinates is the convention of WebAssembly script
// engines: LF, CRLF and lone CR delimit rows, columns count Unicode scalar
// values and ends are exclusive.
var UniversalNewlineCoordinates = toolmanifest.Coordinates{Rows: toolmanifest.RowsSource, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndExclusive}

// Snippet is the source of one check, split into lines as its engine counts
// them. Construct it with NewSnippet.
type Snippet struct {
	convention toolmanifest.Coordinates
	lines      []engineLine
}

// engineLine is one line as the engine counts lines, without its line break,
// and the contract position of its first character.
type engineLine struct {
	text  string
	start sourcetext.Position
}

// NewSnippet splits source into the lines an engine using convention counts.
func NewSnippet(source string, convention toolmanifest.Coordinates) Snippet {
	s := Snippet{convention: convention}
	position := sourcetext.Position{Line: 1, Column: 1}
	line := engineLine{start: position}
	begin := 0
	for i, character := range source {
		loneCR := convention.Rows == toolmanifest.RowsSource && character == '\r' && (i+1 == len(source) || source[i+1] != '\n')
		switch {
		case character == '\n':
			end := i
			if convention.Rows == toolmanifest.RowsSource && end > begin && source[end-1] == '\r' {
				end--
			}
			line.text = source[begin:end]
			s.lines = append(s.lines, line)
			position = sourcetext.Position{Line: position.Line + 1, Column: 1}
			line, begin = engineLine{start: position}, i+1
		case loneCR:
			// The engine starts a line here; the contract continues the LF row.
			line.text = source[begin:i]
			s.lines = append(s.lines, line)
			position.Column++
			line, begin = engineLine{start: position}, i+1
		default:
			position.Column++
		}
	}
	line.text = source[begin:]
	s.lines = append(s.lines, line)
	return s
}

// Normalize validates d and converts its snippet positions, in place, from
// the engine's convention to the contract's. A diagnostic in a reported
// source keeps its positions, which belong to that file.
//
// The rules every transport applies:
//   - a location starts at row 1, column 1 or later;
//   - an end location is absent (zero), or valid and not before the location;
//   - a fix has at least one replacement;
//   - a replacement has both a start and an end, in that order;
//   - every snippet position, of the diagnostic and of its replacements, lies
//     inside the snippet: on one of its rows and at most one column past the
//     end of that row.
//
// A reported source is a file the host has not seen yet, so only the first
// two rules apply to its positions.
func (s Snippet) Normalize(d *scriptengine.Diagnostic) error {
	if err := validRange(d.Location, d.EndLocation, false); err != nil {
		return err
	}
	if d.Source != "" {
		return nil
	}
	if !s.contains(d.Location) || d.EndLocation != (sourcetext.Position{}) && !s.contains(d.EndLocation) {
		return fmt.Errorf("range %v-%v lies outside the snippet", d.Location, d.EndLocation)
	}
	for _, fix := range d.Fixes {
		if len(fix.Replacements) == 0 {
			return errors.New("fix has no replacements")
		}
		for _, r := range fix.Replacements {
			if err := validRange(r.Start, r.End, true); err != nil {
				return fmt.Errorf("fix replacement: %w", err)
			}
			if !s.contains(r.Start) || !s.contains(r.End) {
				return fmt.Errorf("fix replacement %v-%v lies outside the snippet", r.Start, r.End)
			}
		}
	}
	d.Location = s.start(d.Location)
	d.EndLocation = s.end(d.EndLocation)
	for i := range d.Fixes {
		for j := range d.Fixes[i].Replacements {
			r := &d.Fixes[i].Replacements[j]
			r.Start, r.End = s.start(r.Start), s.end(r.End)
		}
	}
	return nil
}

// validRange accepts a positive start and an end that is positive and does
// not precede it. An absent (zero) end is accepted unless endRequired.
func validRange(start, end sourcetext.Position, endRequired bool) error {
	if start.Line < 1 || start.Column < 1 {
		return fmt.Errorf("invalid start %v", start)
	}
	if end == (sourcetext.Position{}) && !endRequired {
		return nil
	}
	if end.Line < 1 || end.Column < 1 || end.IsBefore(start) {
		return fmt.Errorf("invalid range %v-%v", start, end)
	}
	return nil
}

// contains reports whether position names a character of the snippet or the
// end of one of its lines.
func (s Snippet) contains(position sourcetext.Position) bool {
	return position.Line <= len(s.lines) && position.Column <= s.units(s.lines[position.Line-1].text)+1
}

// units measures text in the engine's column unit.
func (s Snippet) units(text string) int {
	switch s.convention.Columns {
	case toolmanifest.ColumnsBytes:
		return len(text)
	case toolmanifest.ColumnsUTF16:
		return len(utf16.Encode([]rune(text)))
	default:
		return utf8.RuneCountInString(text)
	}
}

func (s Snippet) start(position sourcetext.Position) sourcetext.Position {
	return s.convert(position, false)
}

// end converts an end position; the zero position means no end.
func (s Snippet) end(position sourcetext.Position) sourcetext.Position {
	if position == (sourcetext.Position{}) {
		return position
	}
	return s.convert(position, s.convention.End == toolmanifest.EndInclusive)
}

// convert accepts a position that Normalize found inside the snippet.
func (s Snippet) convert(position sourcetext.Position, inclusive bool) sourcetext.Position {
	line := s.lines[position.Line-1]
	units, characters := position.Column-1, 0
	text := line.text
	for units > 0 && text != "" {
		character, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		switch s.convention.Columns {
		case toolmanifest.ColumnsBytes:
			units -= size
		case toolmanifest.ColumnsUTF16:
			units -= utf16.RuneLen(character)
		default:
			units--
		}
		characters++
	}
	// One column past the line's end is the line's end.
	characters += max(units, 0)
	converted := sourcetext.Position{Line: line.start.Line, Column: line.start.Column + characters}
	if inclusive {
		converted.Column++
	}
	return converted
}

// ValidReportedSource reports whether a sandboxed engine may name a file it
// read as name: a relative slash-separated path below the request directory,
// with no "." or ".." elements and no NUL. Backslashes and colons are
// rejected because a Windows host would read the name as a different path
// than the engine opened.
func ValidReportedSource(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00")
}
