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
