package results

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
)

type PublishedDiagnostic struct {
	Diagnostic
	Source  DiagnosticSource `json:"-"`
	Excerpt Excerpt          `json:"-"`
}

type ExcerptKind uint8

const (
	ExcerptAbsent ExcerptKind = iota
	// ExcerptEmptySource means the diagnostic starts on the only line of an
	// empty source, with no captured snippet.
	ExcerptEmptySource
	ExcerptText
)

// Excerpt is the line where a diagnostic starts and the inclusive column where
// the indicator under that line ends.
type Excerpt struct {
	Kind      ExcerptKind
	Line      string
	EndColumn int
}

// Publish keeps a Snippet already on the diagnostic when the source supplies
// no start line text.
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

// Diagnostics returns nil only when published is nil.
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
