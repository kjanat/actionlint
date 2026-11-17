// Package scripts describes declared script text, its source mapping and resolved execution settings.
package scripts

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// Declaration is a script's source data, independent of how often it is invoked.
// Shell is the declared shell, before applying runner defaults. Start and Spans
// identify the original source at Path; Spans index the bytes of Text. JobID and
// StepID identify its declarations when those declarations have IDs.
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

// Shell is the shell command a run step executes under. Implicit marks the
// runner's bash -e fallback or a platform default, which differ from an
// explicit bash by omitting pipefail; custom templates supply their own
// options. The zero value means static analysis could not resolve the shell.
type Shell struct {
	Name     string
	Implicit bool
}

// Known reports whether static analysis resolved the shell.
func (s Shell) Known() bool { return s.Name != "" }

// Execution carries the shell and working directory the runner would use.
// DirectoryKnown reports whether static analysis determined the directory.
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

// OffsetPosition maps a byte offset of Text to the declaring document.
func (s Declaration) OffsetPosition(offset int, exclusive bool) (sourcetext.Position, bool) {
	return sourcemap.Locate(s.Text, s.Spans, offset, exclusive)
}
