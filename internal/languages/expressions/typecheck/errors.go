package typecheck

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

// Error is a syntax or semantic finding in an expression. Rule and Code identify
// the check independently of its wording and source coordinates.
type Error struct {
	// Message describes the finding.
	Message string
	// Offset is the byte offset where the finding starts.
	Offset int
	// EndOffset is the exclusive byte end of the diagnostic's intended range.
	EndOffset int
	// Position is where the finding starts in the expression.
	Position sourcetext.Position
	// Rule identifies the configurable check; Code identifies its diagnostic kind.
	Rule string
	Code string
	// Advisory marks a finding that does not invalidate the expression.
	Advisory bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d:%d: %s", e.Position.Line, e.Position.Column, e.Offset, e.Message)
}

func (e *Error) String() string {
	return e.Error()
}

// detached copies e for a caller. Callers rebase positions onto their
// documents in place; a copy keeps that from rewriting the error a lexer,
// parser or checker still holds and may report again.
func (e *Error) detached() *Error {
	if e == nil {
		return nil
	}
	copied := *e
	return &copied
}

// detachErrors copies each error in errs into fresh storage.
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
