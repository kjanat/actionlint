package yamlsource

// TokenEndColumn returns the exclusive rune column of the whitespace-delimited
// token at column. Invalid or empty positions retain a one-character span.
func TokenEndColumn(line string, column int) int {
	if column < 1 {
		return column + 1
	}
	current, end := 1, column
	for _, r := range line {
		if current >= column {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				break
			}
			end++
		}
		current++
	}
	return max(column+1, end)
}

// ByteOffsetAtColumn converts a 1-based character column to a byte offset. The column immediately
// after the final character is valid because external analyzers use it as an exclusive range end.
func ByteOffsetAtColumn(s string, column int) (int, bool) {
	if column <= 0 {
		return 0, false
	}
	c := 1
	for offset := range s {
		if c == column {
			return offset, true
		}
		c++
	}
	if c == column {
		return len(s), true
	}
	return 0, false
}
