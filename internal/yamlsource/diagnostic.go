package yamlsource

import "strings"

func DiagnosticLine(source []byte, line int, rawScript bool) (string, bool) {
	if !rawScript {
		return SourceLine(string(source), line)
	}
	current := 0
	for text := range strings.SplitSeq(string(source), "\n") {
		current++
		if current == line {
			return strings.TrimSuffix(text, "\r"), true
		}
	}
	return "", false
}

// InclusiveEnd returns the explicit inclusive end column, or the end of the
// whitespace-delimited token at column.
func InclusiveEnd(line string, column, explicit int) int {
	if explicit >= column {
		return explicit
	}
	return TokenEndColumn(line, column) - 1
}
