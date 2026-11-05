package text

import "bytes"

// LFLine returns a one-based line for diagnostic display from raw LF-delimited
// source. It removes the CR in CRLF, retains a bare CR at EOF, and treats a
// trailing LF as an additional empty line. Coordinate helpers use untrimmed
// input for byte offsets and Unicode columns.
func LFLine(source []byte, line int) (string, bool) {
	if line < 1 {
		return "", false
	}
	remaining := line
	start := 0
	for {
		relativeEnd := bytes.IndexByte(source[start:], '\n')
		if relativeEnd < 0 {
			if remaining == 1 {
				return string(source[start:]), true
			}
			return "", false
		}

		end := start + relativeEnd
		if remaining == 1 {
			if end > start && source[end-1] == '\r' {
				end--
			}
			return string(source[start:end]), true
		}
		remaining--
		start = end + 1
	}
}
