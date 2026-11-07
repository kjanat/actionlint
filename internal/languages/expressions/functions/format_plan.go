package functions

import (
	"iter"
	"strconv"
	"strings"
)

type FormatPart struct {
	Text      string
	Index     int // -1 identifies literal text.
	Specifier string
}

type FormatPlan struct {
	parts []FormatPart
	valid bool
}

func (plan FormatPlan) References(index int) bool {
	for _, part := range plan.parts {
		if part.Index == index {
			return true
		}
	}
	return false
}

func (plan FormatPlan) Demands(index int) bool {
	for _, part := range plan.parts {
		if part.Index == index {
			return true
		}
		if part.Specifier != "" {
			return false
		}
	}
	return false
}

// PlanFormat validates the entire template before any replacement is evaluated.
// Escaped braces are literal text; placeholders contain unsigned decimal indexes.
func PlanFormat(format string, count int) FormatPlan {
	plan := FormatPlan{valid: true}
	var literal strings.Builder
	flush := func() {
		if literal.Len() != 0 {
			plan.parts = append(plan.parts, FormatPart{Text: literal.String(), Index: -1})
			literal.Reset()
		}
	}
	for i := 0; i < len(format); {
		c := format[i]
		if c != '{' && c != '}' {
			literal.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(format) && format[i+1] == c {
			literal.WriteByte(c)
			i += 2
			continue
		}
		end := i + 1
		for end < len(format) && format[end] >= '0' && format[end] <= '9' {
			end++
		}
		if c != '{' || end == i+1 || end >= len(format) || (format[end] != '}' && format[end] != ':') {
			plan.valid = false
			return plan
		}
		index, err := strconv.Atoi(format[i+1 : end])
		if err != nil || index > 255 {
			plan.valid = false
			return plan
		}
		specifier := ""
		if format[end] == ':' {
			var spec strings.Builder
			end++
		scanSpecifier:
			for end < len(format) {
				switch {
				case format[end] != '}':
					spec.WriteByte(format[end])
					end++
				case end+1 < len(format) && format[end+1] == '}':
					spec.WriteByte('}')
					end += 2
				default:
					break scanSpecifier
				}
			}
			if end == len(format) {
				plan.valid = false
				return plan
			}
			specifier = spec.String()
		}
		flush()
		plan.parts = append(plan.parts, FormatPart{Index: index, Specifier: specifier})
		if index >= count {
			plan.valid = false
		}
		i = end + 1
	}
	flush()
	return plan
}

// Valid reports whether the full template and every placeholder index are valid.
func (plan FormatPlan) Valid() bool { return plan.valid }

// Parts yields literal text and placeholders in evaluation order.
func (plan FormatPlan) Parts() iter.Seq[FormatPart] {
	return func(yield func(FormatPart) bool) {
		for _, part := range plan.parts {
			if !yield(part) {
				return
			}
		}
	}
}
