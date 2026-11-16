package sourcetext

import "fmt"

// Position uses one-based Unicode-scalar line and column numbers.
// Range ends are exclusive and may lie on a later line.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// IsBefore reports whether p precedes other. Equal positions return false.
func (p Position) IsBefore(other Position) bool {
	if p.Line != other.Line {
		return p.Line < other.Line
	}
	return p.Column < other.Column
}

// String formats p as line:L,col:C for diagnostics.
func (p Position) String() string {
	return fmt.Sprintf("line:%d,col:%d", p.Line, p.Column)
}

// ZeroIfNil returns the zero Position, which marks an absent location, when p is nil.
func ZeroIfNil(p *Position) Position {
	if p == nil {
		return Position{}
	}
	return *p
}

// NilUnlessPresent returns a pointer to p, or nil when present is false.
func NilUnlessPresent(p Position, present bool) *Position {
	if !present {
		return nil
	}
	return &p
}
