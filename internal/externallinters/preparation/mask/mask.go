// Package mask hides workflow template expressions from script tools while
// keeping every row and column of the script in place.
package mask

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

const ID = "kjanat:actionlint/expression-mask@0.1.0"

// Expressions replaces each ${{ }} template with underscores of the same rune
// count and keeps its line breaks. Spaces would change the script's syntax,
// for example `if ${{ contains(xs, s) }}; then`. A `}}` inside a string
// literal such as ${{ '}}' }} does not end the template.
func Expressions(text string) string {
	var out strings.Builder
	for {
		start := strings.Index(text, "${{")
		if start < 0 {
			if out.Len() == 0 {
				return text
			}
			out.WriteString(text)
			return out.String()
		}
		length, ok := end(text[start+3:])
		if !ok {
			if out.Len() == 0 {
				return text
			}
			out.WriteString(text)
			return out.String()
		}
		stop := start + 3 + length
		out.WriteString(text[:start])
		writeMask(&out, text[start:stop])
		text = text[stop:]
	}
}

func writeMask(out *strings.Builder, template string) {
	for _, r := range template {
		if r == '\n' || r == '\r' {
			out.WriteRune(r)
		} else {
			out.WriteByte('_')
		}
	}
}

func end(src string) (int, bool) {
	if _, offset, err := syntax.LexExpression(src); err == nil {
		return offset, true
	}
	if index := strings.Index(src, "}}"); index >= 0 {
		return index + 2, true
	}
	return 0, false
}
