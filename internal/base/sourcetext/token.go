package sourcetext

// TokenEndColumn returns an exclusive rune column, ends tokens at space, tab, CR or LF, and spans at least one character.
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

// ByteOffsetAtColumn accepts the exclusive EOF column and rejects nonpositive and later columns.
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

// InclusiveEnd returns explicit when it is at or beyond column and never clamps to the line.
func InclusiveEnd(line string, column, explicit int) int {
	if explicit >= column {
		return explicit
	}
	return TokenEndColumn(line, column) - 1
}
