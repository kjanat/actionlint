// Package scripts describes declared script text, its source mapping and resolved execution settings.
package scripts

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// Declaration is a script's source data, independent of how often it is invoked.
// Shell is the declared shell, before applying runner defaults. Start and Spans
// identify the original source at Path. JobID and StepID identify its declarations
// when those declarations have IDs.
type Declaration struct {
	Reference sourcepos.Reference
	JobID     string
	StepID    string
	Text      string
	Shell     string
	Start     sourcepos.Position
	Path      string
	Spans     []Span
	Comments  []workflowfacts.SourceComment
}

// Span maps the text columns [Start, End) of one line of Text to the declaring
// document. Document is the position of the first mapped character. DocumentEnd
// is nonzero for a decoded escape sequence, whose source width differs from its
// text width.
type Span struct {
	Line        int
	Start       int
	End         int
	Document    sourcepos.Position
	DocumentEnd sourcepos.Position
}

// Execution carries the shell and working directory the runner would use.
// ShellKnown and DirectoryKnown report whether static analysis determined the
// value. ShellImplicit marks a shell supplied by the runner or platform.
type Execution struct {
	Shell          string
	ShellKnown     bool
	ShellImplicit  bool
	Directory      string
	DirectoryKnown bool
}

// Position maps a one-based line and character column of Text to the declaring
// document. Exclusive selects the end of a range. Text without a source, such as
// whitespace synthesized by YAML folding, reports false.
func (s Declaration) Position(line, column int, exclusive bool) (sourcepos.Position, bool) {
	for _, span := range s.Spans {
		if span.Line != line {
			continue
		}
		inside := column >= span.Start && column < span.End
		if exclusive {
			inside = column > span.Start && column <= span.End || span.Start == span.End && column == span.Start
		}
		if !inside {
			continue
		}
		if span.DocumentEnd.Line != 0 {
			if exclusive && column == span.End {
				return span.DocumentEnd, true
			}
			return span.Document, true
		}
		return sourcepos.Position{Line: span.Document.Line, Column: span.Document.Column + column - span.Start}, true
	}
	return sourcepos.Position{}, false
}
