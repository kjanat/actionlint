// Package mask hides workflow template expressions from script tools while
// keeping every row and column of the script in place.
package mask

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/expression"
)

// ID names the generic profile in tool provider manifests: it masks
// templates and passes inline settings through, for tools of any language.
const ID = "kjanat:actionlint/expression-mask@0.1.0"

// Expressions replaces each ${{ }} template with underscores of the same rune
// count, keeping its line breaks. Spaces would change the script's syntax,
// for example `if ${{ contains(xs, s) }}; then`.
//
// The expression lexer finds where a template ends, so a `}}` inside a string
// literal such as ${{ '}}' }} does not end it. A template the lexer rejects
// ends at its first `}}`; text after a template without one is left as is.
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
		for _, r := range text[start:stop] {
			if r == '\n' || r == '\r' {
				out.WriteRune(r)
			} else {
				out.WriteByte('_')
			}
		}
		text = text[stop:]
	}
}

// end returns the byte length of the template body that src starts with,
// including its closing `}}`.
func end(src string) (int, bool) {
	if _, offset, err := expression.LexExpression(src); err == nil {
		return offset, true
	}
	if index := strings.Index(src, "}}"); index >= 0 {
		return index + 2, true
	}
	return 0, false
}
