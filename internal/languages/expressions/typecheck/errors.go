package typecheck

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

type Error struct {
	Message string
	Offset  int
	// EndOffset is the exclusive byte end of the diagnostic's intended range.
	EndOffset int
	Position  sourcetext.Position
	Rule      string
	Code      string
	// Advisory marks a finding that does not invalidate the expression.
	Advisory bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d:%d: %s", e.Position.Line, e.Position.Column, e.Offset, e.Message)
}

func (e *Error) String() string {
	return e.Error()
}

// Callers rebase positions in place, and the lexer, parser or checker may still report the original.
func (e *Error) detached() *Error {
	if e == nil {
		return nil
	}
	copied := *e
	return &copied
}

func detachErrors(errs []*Error) []*Error {
	out := make([]*Error, len(errs))
	for i, err := range errs {
		out[i] = err.detached()
	}
	return out
}

func errorAtToken(kind findingKind, t syntax.Token, msg string) *Error {
	return &Error{
		Message: msg,
		Rule:    kind.rule, Code: kind.code,
		Offset:    t.Offset,
		EndOffset: t.Offset + len(t.Value),
		Position:  t.Position,
	}
}
