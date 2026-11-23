package codec

import (
	"errors"
)

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

func RequiresAdmission(acceptedAliases int) bool { return acceptedAliases != 0 }

func NewAdmissionError(cause error) error {
	if cause == nil {
		return nil
	}
	return &AdmissionError{cause: cause}
}

func Admit(root *Node, follow func(parent *Node, index int) bool) error {
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

func admit(cursor Cursor, follow func(*Node, int) bool) error {
	entered, err := cursor.Enter()
	if err != nil {
		return err
	}
	node := entered.Node()
	for index, child := range node.Content {
		if child.Kind == AliasNode && !follow(node, index) {
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
