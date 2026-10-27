package render

import (
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/source/text"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

var (
	bold   = new(color.Color).Add(color.Bold)
	green  = new(color.Color).Add(color.FgGreen)
	yellow = new(color.Color).Add(color.FgYellow)
	gray   = new(color.Color).Add(color.FgHiBlack)
)

// Finding is one diagnostic together with the source its snippet is rendered from.
// EndColumn is the inclusive column where the indicator ends; zero infers the
// token end. RawScript selects LF line splitting for external checker sources.
type Finding struct {
	Diagnostic       diagnostic.Diagnostic
	Source           []byte
	EndColumn        int
	RawScript        bool
	Unavailable      bool
	canonicalSnippet string
}

// FindingOf adapts a canonical half-open span to the inclusive legacy columns.
func FindingOf(d diagnostic.Diagnostic, source []byte) Finding {
	f := Finding{Diagnostic: d, Source: source, canonicalSnippet: d.Snippet}
	if d.End.Line == d.Start.Line && d.End.Column > d.Start.Column {
		f.EndColumn = d.End.Column - 1
	}
	return f
}

func (f Finding) line() (string, bool) {
	if f.canonicalSnippet != "" {
		return f.canonicalSnippet, true
	}
	return sourcemap.DiagnosticLine(f.Source, f.Diagnostic.Start.Line, f.RawScript)
}

func (f Finding) end(line string) int {
	if f.Unavailable {
		return max(f.Diagnostic.Start.Column, f.EndColumn)
	}
	return text.InclusiveEnd(line, f.Diagnostic.Start.Column, f.EndColumn)
}

func (f Finding) indicator(line string) string {
	column := f.Diagnostic.Start.Column
	if column <= 0 {
		return ""
	}
	start, ok := text.ByteOffsetAtColumn(line, column)
	if !ok {
		return ""
	}
	width := 0
	if end := f.end(line); end >= column {
		offset, ok := text.ByteOffsetAtColumn(line, end+1)
		if !ok {
			offset = len(line)
		}
		width = runewidth.StringWidth(line[start:offset])
	}
	if width > 0 {
		width--
	}
	return fmt.Sprintf("%s^%s", strings.Repeat(" ", runewidth.StringWidth(line[:start])), strings.Repeat("~", width))
}

// TemplateFields prepares the finding for a custom Go template.
func (f Finding) TemplateFields() *TemplateFields {
	d := f.Diagnostic
	snippet := ""
	end := d.Start.Column
	if (len(f.Source) > 0 || f.canonicalSnippet != "") && d.Start.Line > 0 {
		if line, ok := f.line(); ok {
			snippet = line
			if indicator := f.indicator(line); indicator != "" {
				snippet += "\n" + indicator
				end = f.end(line)
			}
		}
	}
	return &TemplateFields{
		Message: d.Message, Filepath: d.Path, Line: d.Start.Line, Column: d.Start.Column,
		Kind: d.Rule, Snippet: snippet, EndColumn: end,
	}
}

// PrettyPrint writes the location, message, rule and an indicated source line.
// A nil Source omits the snippet. Set fatih/color.NoColor to disable colors.
func (f Finding) PrettyPrint(w io.Writer) {
	d := f.Diagnostic
	_, _ = yellow.Fprint(w, d.Path)
	_, _ = gray.Fprint(w, ":")
	_, _ = fmt.Fprint(w, d.Start.Line)
	_, _ = gray.Fprint(w, ":")
	_, _ = fmt.Fprint(w, d.Start.Column)
	_, _ = gray.Fprint(w, ": ")
	_, _ = bold.Fprint(w, d.Message)
	_, _ = gray.Fprintf(w, " [%s]\n", d.Rule)

	if (len(f.Source) == 0 && f.canonicalSnippet == "") || d.Start.Line <= 0 {
		return
	}
	line, ok := f.line()
	if !ok || len(line) < d.Start.Column-1 {
		return
	}
	number := fmt.Sprintf("%d | ", d.Start.Line)
	indent := strings.Repeat(" ", len(number)-2)
	_, _ = gray.Fprintf(w, "%s|\n", indent)
	_, _ = gray.Fprint(w, number)
	_, _ = fmt.Fprintln(w, line)
	_, _ = gray.Fprintf(w, "%s| ", indent)
	_, _ = green.Fprintln(w, f.indicator(line))
}
