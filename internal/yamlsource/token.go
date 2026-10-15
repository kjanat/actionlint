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
