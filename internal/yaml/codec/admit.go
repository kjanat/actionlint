package codec

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// AdmissionError prevents semantic construction while preserving the load cause.
type AdmissionError struct{ cause error }

func (err *AdmissionError) Error() string {
	if err == nil || err.cause == nil {
		return "YAML admission rejected"
	}
	return err.cause.Error()
}

func (err *AdmissionError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// RequiresAdmission follows the session's alias-only resource policy. Without
// accepted crossings its alias counter stays zero and cannot reject a document.
func RequiresAdmission(acceptedAliases int) bool { return acceptedAliases != 0 }

// NewAdmissionError preserves a rejected admission's cause; nil means no rejection.
func NewAdmissionError(cause error) error {
	if cause == nil {
		return nil
	}
	return &AdmissionError{cause: cause}
}

// Admit checks expansion cost without constructing values or changing the graph.
// follow selects alias slots according to the caller's dialect. Rejected slots
// are neither followed nor counted; accepted expansions share the session limit.
func Admit(root *syntax.Node, follow func(parent *syntax.Node, index int) bool) error {
	if follow == nil {
		return NewAdmissionError(errors.New("YAML admission requires an alias selection policy"))
	}
	session, cursor, err := Open(root)
	if err != nil {
		return NewAdmissionError(err)
	}
	defer session.Close()
	if err := session.Finish(admit(cursor, follow)); err != nil {
		return NewAdmissionError(err)
	}
	return nil
}

func admit(cursor Cursor, follow func(*syntax.Node, int) bool) error {
	entered, err := cursor.Enter()
	if err != nil {
		return err
	}
	node := entered.Node()
	for index, child := range node.Content {
		if child.Kind == syntax.AliasNode && !follow(node, index) {
			continue
		}
		next := entered.cursor
		next.node = child
		if err := admit(next, follow); err != nil {
			return err
		}
	}
	return nil
}
