package sourcetext

import "fmt"

// Position uses one-based Unicode-scalar line and column numbers.
// Range ends are exclusive and may lie on a later line.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func (p Position) IsBefore(other Position) bool {
	if p.Line != other.Line {
		return p.Line < other.Line
	}
	return p.Column < other.Column
}

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

func NilUnlessPresent(p Position, present bool) *Position {
	if !present {
		return nil
	}
	return &p
}
