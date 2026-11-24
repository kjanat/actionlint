// Package scripts describes declared script text, its source mapping and resolved execution settings.
package scripts

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// Declaration holds the declared Shell before runner defaults. Spans index the bytes of Text.
type Declaration struct {
	Reference sourcetext.Reference
	JobID     string
	StepID    string
	Text      string
	Shell     string
	Start     sourcetext.Position
	Path      string
	Spans     []sourcemap.Span
	Comments  []facts.SourceComment
}

// Shell is a run step's shell. Implicit marks the runner's bash -e fallback or a
// platform default, which omit pipefail. The zero value means the shell is unknown.
type Shell struct {
	Name     string
	Implicit bool
}

func (s Shell) Known() bool { return s.Name != "" }

type Execution struct {
	Shell          Shell
	Directory      string
	DirectoryKnown bool
}

// Position maps a one-based line and character column of Text to the declaring
// document. Exclusive selects the end of a range. Text without a source, such as
// whitespace synthesized by YAML folding, reports false.
func (s Declaration) Position(line, column int, exclusive bool) (sourcetext.Position, bool) {
	if line < 1 {
		return sourcetext.Position{}, false
	}
	start := 0
	for range line - 1 {
		next := strings.IndexByte(s.Text[start:], '\n')
		if next < 0 {
			return sourcetext.Position{}, false
		}
		start += next + 1
	}
	text := s.Text[start:]
	if end := strings.IndexByte(text, '\n'); end >= 0 {
		text = text[:end]
	}
	offset, ok := sourcetext.ByteOffsetAtColumn(text, column)
	if !ok {
		return sourcetext.Position{}, false
	}
	return s.OffsetPosition(start+offset, exclusive)
}

func (s Declaration) OffsetPosition(offset int, exclusive bool) (sourcetext.Position, bool) {
	return sourcemap.Locate(s.Text, s.Spans, offset, exclusive)
}
