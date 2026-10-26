package text

import "strings"

// LFLine returns a one-based line from raw LF-delimited source, trimming the
// CR from CRLF without interpreting other Unicode line separators.
func LFLine(source []byte, line int) (string, bool) {
	if line < 1 {
		return "", false
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
