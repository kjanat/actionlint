package mask

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
)

type Variant struct {
	Text          string
	Substitutions []preparation.Substitution
}

// Variants returns one script for each value of the template with the most
// known values. Variant i gives each template with known values its value at
// i modulo their number and masks every other template, as Expressions does.
// A template spanning rows, or with a value spanning rows, stays masked.
func Variants(text string) []Variant {
	type template struct {
		start, stop int
		values      []string
		row, column int
	}
	var templates []template
	runs := 0
	row, counted := 1, 0
	for offset := 0; ; {
		start := strings.Index(text[offset:], "${{")
		if start < 0 {
			break
		}
		start += offset
		length, ok := end(text[start+3:])
		if !ok {
			break
		}
		t := template{start: start, stop: start + 3 + length}
		row, counted = row+strings.Count(text[counted:start], "\n"), start
		t.row, t.column = row, utf8.RuneCountInString(text[strings.LastIndexByte(text[:start], '\n')+1:start])+1
		if source := text[t.start:t.stop]; !strings.ContainsAny(source, "\r\n") {
			if values, known := evaluate.TemplateStringAlternatives(source); known && !slices.ContainsFunc(values, rowBreak) {
				t.values = values
				runs = max(runs, len(values))
			}
		}
		templates = append(templates, t)
		offset = t.stop
	}
	if runs == 0 {
		return nil
	}
	variants := make([]Variant, runs)
	for i := range variants {
		var out strings.Builder
		previous := 0
		for _, t := range templates {
			out.WriteString(text[previous:t.start])
			previous = t.stop
			if t.values == nil {
				writeMask(&out, text[t.start:t.stop])
				continue
			}
			value := t.values[i%len(t.values)]
			out.WriteString(value)
			variants[i].Substitutions = append(variants[i].Substitutions, preparation.Substitution{Template: text[t.start:t.stop], Value: value, Row: t.row, Column: t.column, Alternatives: len(t.values)})
		}
		out.WriteString(text[previous:])
		variants[i].Text = out.String()
	}
	return variants
}

func rowBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}
