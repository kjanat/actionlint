// Package text handles Unicode columns and LF-delimited source lines.
package text

// TokenEndColumn returns the exclusive rune column of the token delimited by
// space, tab, CR or LF at column, with a minimum span of one character.
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

// ByteOffsetAtColumn converts a one-based rune column to a byte offset.
// The exclusive EOF column is valid; nonpositive and later columns are invalid.
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

// InclusiveEnd returns explicit unchanged when it is at or beyond column.
// Earlier values request token inference. It does not clamp to the line.
func InclusiveEnd(line string, column, explicit int) int {
	if explicit >= column {
		return explicit
	}
	return TokenEndColumn(line, column) - 1
}
