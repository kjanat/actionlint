package output

import (
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/results"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

// RenderedFinding is one diagnostic together with the source its snippet is rendered from.
// Source.RawScript selects LF line splitting for external checker sources.
// Source.Unavailable retains only a reported range or a single caret.
type RenderedFinding struct {
	Diagnostic       results.Diagnostic
	Source           results.DiagnosticSource
	canonicalSnippet string
}

// RenderedFindingOf renders a diagnostic from the given source text.
func RenderedFindingOf(d results.Diagnostic, content []byte) RenderedFinding {
	return RenderedFinding{Diagnostic: d, Source: results.DiagnosticSource{Content: content}, canonicalSnippet: d.Snippet}
}

func (f RenderedFinding) line() (string, bool) {
	if f.Source.Content != nil {
		return sourcemap.DiagnosticLine(f.Source.Content, f.Diagnostic.Start.Line, f.Source.RawScript)
	}
	if f.canonicalSnippet != "" {
		return f.canonicalSnippet, true
	}
	return sourcemap.DiagnosticLine(f.Source.Content, f.Diagnostic.Start.Line, f.Source.RawScript)
}

// explicitEndColumn converts the diagnostic's half-open end to the inclusive
// column where the indicator ends. Zero means the end was not reported on the
// start line, so the token end is inferred.
func (f RenderedFinding) explicitEndColumn() int {
	d := f.Diagnostic
	if d.End.Line == d.Start.Line && d.End.Column > d.Start.Column {
		return d.End.Column - 1
	}
	return 0
}

func (f RenderedFinding) end(line string) int {
	if f.Source.Unavailable.Present() {
		return max(f.Diagnostic.Start.Column, f.explicitEndColumn())
	}
	return source.InclusiveEnd(line, f.Diagnostic.Start.Column, f.explicitEndColumn())
}

func (f RenderedFinding) indicator(line string) string {
	column := f.Diagnostic.Start.Column
	if column <= 0 {
		return ""
	}
	start, ok := source.ByteOffsetAtColumn(line, column)
	if !ok {
		return ""
	}
	width := 0
	if end := f.end(line); end >= column {
		offset, ok := source.ByteOffsetAtColumn(line, end+1)
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
func (f RenderedFinding) TemplateFields() *TemplateFields {
	d := f.Diagnostic
	snippet := ""
	end := d.Start.Column
	if (len(f.Source.Content) > 0 || f.canonicalSnippet != "") && d.Start.Line > 0 {
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
		Kind: d.Rule, Code: d.Code, Severity: d.Severity, Snippet: snippet, EndColumn: end,
	}
}

// PrettyPrint writes the location, message, rule and an indicated source line.
// Without source bytes, the diagnostic's captured snippet is used when available.
func (f RenderedFinding) PrettyPrint(w io.Writer) {
	f.prettyPrint(w, false)
}

func (f RenderedFinding) prettyPrint(w io.Writer, colored bool) {
	bold := colorStyle(colored, color.Bold)
	green := colorStyle(colored, color.FgGreen)
	yellow := colorStyle(colored, color.FgYellow)
	gray := colorStyle(colored, color.FgHiBlack)
	d := f.Diagnostic
	_, _ = yellow.Fprint(w, d.Path)
	_, _ = gray.Fprint(w, ":")
	_, _ = fmt.Fprint(w, d.Start.Line)
	_, _ = gray.Fprint(w, ":")
	_, _ = fmt.Fprint(w, d.Start.Column)
	_, _ = gray.Fprint(w, ": ")
	if d.Code != "" && d.Code != d.Rule {
		_, _ = bold.Fprintf(w, "%s: %s", d.Code, d.Message)
	} else {
		_, _ = bold.Fprint(w, d.Message)
	}
	_, _ = gray.Fprintf(w, " [%s]\n", d.Rule)

	if (len(f.Source.Content) == 0 && f.canonicalSnippet == "") || d.Start.Line <= 0 {
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
