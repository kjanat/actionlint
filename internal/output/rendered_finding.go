package output

import (
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

// indicator underlines the finding's range in its excerpt line, from the start
// column to the excerpt's inclusive end column.
func indicator(f results.PublishedDiagnostic) string {
	line, column := f.Excerpt.Line, f.Start.Column
	if column <= 0 {
		return ""
	}
	start, ok := sourcetext.ByteOffsetAtColumn(line, column)
	if !ok {
		return ""
	}
	width := 0
	if end := f.Excerpt.EndColumn; end >= column {
		offset, ok := sourcetext.ByteOffsetAtColumn(line, end+1)
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

// TemplateFieldsOf prepares the finding for a custom Go template.
func TemplateFieldsOf(f results.PublishedDiagnostic) *TemplateFields {
	d := f.Diagnostic
	snippet := ""
	end := d.Start.Column
	if f.Excerpt.Kind == results.ExcerptText {
		snippet = f.Excerpt.Line
		if indicator := indicator(f); indicator != "" {
			snippet += "\n" + indicator
			end = f.Excerpt.EndColumn
		}
	}
	return &TemplateFields{
		Message: d.Message, Filepath: d.Path, Line: d.Start.Line, Column: d.Start.Column,
		Kind: d.Rule, Code: d.Code, Severity: d.Severity, Snippet: snippet, EndColumn: end,
	}
}

// PrettyPrint writes the location, message, rule and the finding's excerpt
// line with its range indicated.
func PrettyPrint(w io.Writer, f results.PublishedDiagnostic) {
	prettyPrint(w, f, false)
}

func prettyPrint(w io.Writer, f results.PublishedDiagnostic, colored bool) {
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

	line := f.Excerpt.Line
	if f.Excerpt.Kind != results.ExcerptText || len(line) < d.Start.Column-1 {
		return
	}
	number := fmt.Sprintf("%d | ", d.Start.Line)
	indent := strings.Repeat(" ", len(number)-2)
	_, _ = gray.Fprintf(w, "%s|\n", indent)
	_, _ = gray.Fprint(w, number)
	_, _ = fmt.Fprintln(w, line)
	_, _ = gray.Fprintf(w, "%s| ", indent)
	_, _ = green.Fprintln(w, indicator(f))
}
