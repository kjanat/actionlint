package execution

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// LiteralShellWord decodes shell words containing no expansions or substitutions.
func LiteralShellWord(parts []syntax.WordPart) (string, bool) {
	var out strings.Builder
	for _, part := range parts {
		switch value := part.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(value.Value, "\\*?[~{}") {
				return "", false
			}
			out.WriteString(value.Value)
		case *syntax.SglQuoted:
			if value.Dollar {
				return "", false
			}
			out.WriteString(value.Value)
		case *syntax.DblQuoted:
			if value.Dollar {
				return "", false
			}
			for _, part := range value.Parts {
				literal, ok := part.(*syntax.Lit)
				if !ok || strings.ContainsRune(literal.Value, '\\') {
					return "", false
				}
				out.WriteString(literal.Value)
			}
		default:
			return "", false
		}
	}
	return out.String(), true
}
