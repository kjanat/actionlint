// Package scripts describes declared script text, its source mapping and resolved execution settings.
package scripts

import (
	"strings"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// Declaration is a script's source data, independent of how often it is invoked.
// Shell is the declared shell, before applying runner defaults. Start and Spans
// identify the original source at Path; Spans index the bytes of Text. JobID and
// StepID identify its declarations when those declarations have IDs.
type Declaration struct {
	Reference sourcepos.Reference
	JobID     string
	StepID    string
	Text      string
	Shell     string
	Start     sourcepos.Position
	Path      string
	Spans     []sourcemap.Span
	Comments  []workflowfacts.SourceComment
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
	if line < 1 {
		return sourcepos.Position{}, false
	}
	start := 0
	for range line - 1 {
		next := strings.IndexByte(s.Text[start:], '\n')
		if next < 0 {
			return sourcepos.Position{}, false
		}
		start += next + 1
	}
	text := s.Text[start:]
	if end := strings.IndexByte(text, '\n'); end >= 0 {
		text = text[:end]
	}
	offset, ok := sourcepos.ByteOffsetAtColumn(text, column)
	if !ok {
		return sourcepos.Position{}, false
	}
	return s.OffsetPosition(start+offset, exclusive)
}

// OffsetPosition maps a byte offset of Text to the declaring document.
func (s Declaration) OffsetPosition(offset int, exclusive bool) (sourcepos.Position, bool) {
	return sourcemap.Locate(s.Text, s.Spans, offset, exclusive)
}
