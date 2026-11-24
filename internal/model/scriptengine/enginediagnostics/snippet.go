// Package enginediagnostics admits the diagnostics a script engine returns
// for one snippet.
package enginediagnostics

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

var ContractCoordinates = toolmanifest.Coordinates{Rows: toolmanifest.RowsLF, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndExclusive}

var UniversalNewlineCoordinates = toolmanifest.Coordinates{Rows: toolmanifest.RowsSource, Columns: toolmanifest.ColumnsChars, End: toolmanifest.EndExclusive}

// Snippet must be constructed with NewSnippet.
type Snippet struct {
	convention toolmanifest.Coordinates
	lines      []engineLine
}

type engineLine struct {
	text  string
	start sourcetext.Position
}

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
// the engine's convention to the contract's.
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

func (s Snippet) contains(position sourcetext.Position) bool {
	return position.Line <= len(s.lines) && position.Column <= s.units(s.lines[position.Line-1].text)+1
}

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
	characters += max(units, 0)
	converted := sourcetext.Position{Line: line.start.Line, Column: line.start.Column + characters}
	if inclusive {
		converted.Column++
	}
	return converted
}

func ValidReportedSource(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00")
}
