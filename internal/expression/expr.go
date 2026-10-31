package expression

import "fmt"

// Error is a syntax or semantic finding in an expression. Rule and Code identify
// the check independently of its wording and source coordinates.
type Error struct {
	// Message is an Error message
	Message string
	// Offset is byte offset position which caused the Error
	Offset int
	// EndOffset is the exclusive byte end of the diagnostic's intended range.
	EndOffset int
	// Line is line number position which caused the Error. Note that this value is 1-based.
	Line int
	// Column is column number position which caused the Error. Note that this value is 1-based.
	Column int
	// Rule identifies the configurable check; Code identifies its diagnostic kind.
	Rule string
	Code string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d:%d: %s", e.Line, e.Column, e.Offset, e.Message)
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
