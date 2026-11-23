package parser

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

type SyntaxError struct {
	Message  string
	Position sourcetext.Position
	Kind     string
	// Code is empty outside the workflow parser.
	Code Code
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%d:%d: %s [%s]", e.Position.Line, e.Position.Column, e.Message, e.Kind)
}
