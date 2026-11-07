package source

// Position uses one-based Unicode-scalar line and column numbers.
// Range ends are exclusive and may lie on a later line.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}
