package parser

import "fmt"

// SyntaxError describes a syntax failure independently of lint rule state.
type SyntaxError struct {
	Message string
	Line    int
	Column  int
	Kind    string
	// Code names why GitHub rejects the document; empty outside the workflow parser.
	Code Code
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%d:%d: %s [%s]", e.Line, e.Column, e.Message, e.Kind)
}
