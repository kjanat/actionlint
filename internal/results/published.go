package results

import (
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
)

// PublishedDiagnostic is one diagnostic as analysis publishes it: the canonical
// diagnostic, the source text its positions refer to, and the source line
// renderers display. Publish resolves the diagnostic's End and Snippet and the
// Excerpt together, so consumers read them instead of deriving them again.
type PublishedDiagnostic struct {
	Diagnostic
	Source  DiagnosticSource `json:"-"`
	Excerpt Excerpt          `json:"-"`
}

// ExcerptKind tells whether a diagnostic's start line was found, and whether
// it was found in source text or only as the one line of an empty source.
type ExcerptKind uint8

const (
	// ExcerptAbsent means the start line is not available.
	ExcerptAbsent ExcerptKind = iota
	// ExcerptEmptySource means the diagnostic starts on the only line of an
	// empty source, with no captured snippet.
	ExcerptEmptySource
	// ExcerptText means Line is a line of the source or the captured snippet.
	ExcerptText
)

// Excerpt is the line a diagnostic starts on and the inclusive column at
// which the indicator under that line ends.
type Excerpt struct {
	Kind      ExcerptKind
	Line      string
	EndColumn int
}

// Publish pairs a canonical diagnostic with its source. It reads the start line
// once and uses it to resolve a missing End, the published Snippet, and the
// displayed Excerpt. A Snippet already on the diagnostic is kept when the source
// supplies no start line text.
func Publish(d Diagnostic, source DiagnosticSource) PublishedDiagnostic {
	line, found := sourcemap.DiagnosticLine(source.Content, d.Start.Line, source.RawScript)
	unavailable := source.Unavailable.Present()
	if d.End == (sourcetext.Position{}) {
		d.End = sourcetext.Position{Line: d.Start.Line, Column: d.Start.Column + 1}
		switch {
		case unavailable:
			d.End = d.Start
		case line != "":
			d.End.Column = sourcetext.InclusiveEnd(line, d.Start.Column, 0) + 1
		}
	}
	if line != "" {
		d.Snippet = line
	}
	excerpt := Excerpt{}
	switch {
	case d.Start.Line <= 0:
	case source.Content != nil && found:
		excerpt.Kind, excerpt.Line = ExcerptText, line
		if len(source.Content) == 0 && d.Snippet == "" {
			excerpt.Kind = ExcerptEmptySource
		}
	case source.Content == nil && d.Snippet != "":
		excerpt.Kind, excerpt.Line = ExcerptText, d.Snippet
	case source.Content == nil && found:
		excerpt.Kind = ExcerptEmptySource
	}
	if excerpt.Kind != ExcerptAbsent {
		excerpt.EndColumn = inclusiveEndColumn(d, excerpt.Line, unavailable)
	}
	return PublishedDiagnostic{Diagnostic: d, Source: source, Excerpt: excerpt}
}

// Diagnostics returns the canonical diagnostics of published, in order. The
// result is nil only when published is nil.
func Diagnostics(published []PublishedDiagnostic) []Diagnostic {
	if published == nil {
		return nil
	}
	diagnostics := make([]Diagnostic, len(published))
	for i := range published {
		diagnostics[i] = published[i].Diagnostic
	}
	return diagnostics
}

// inclusiveEndColumn returns the column where the indicator under line ends.
// An End on the start line bounds it; otherwise the token at Start does.
func inclusiveEndColumn(d Diagnostic, line string, unavailable bool) int {
	explicit := 0
	if d.End.Line == d.Start.Line && d.End.Column > d.Start.Column {
		explicit = d.End.Column - 1
	}
	if unavailable {
		return max(d.Start.Column, explicit)
	}
	return sourcetext.InclusiveEnd(line, d.Start.Column, explicit)
}
