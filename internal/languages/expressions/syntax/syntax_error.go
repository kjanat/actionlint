package syntax

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

// SyntaxError reports a syntax failure and its range in the decoded expression text.
type SyntaxError struct {
	Message           string
	Offset, EndOffset int
	// Position is where the failure starts in the expression.
	Position sourcetext.Position
}

func (issue *SyntaxError) Error() string {
	return fmt.Sprintf("%d:%d:%d: %s", issue.Position.Line, issue.Position.Column, issue.Offset, issue.Message)
}

func (issue *SyntaxError) String() string { return issue.Error() }

func (issue *SyntaxError) detached() *SyntaxError {
	if issue == nil {
		return nil
	}
	detached := *issue
	return &detached
}

func issueAtToken(token Token, message string) *SyntaxError {
	return &SyntaxError{Message: message, Offset: token.Offset, EndOffset: token.Offset + len(token.Value), Position: token.Position}
}
