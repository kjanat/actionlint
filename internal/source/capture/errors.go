package capture

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrInvalidSnapshot indicates handle misuse, not a source disagreement.
	ErrInvalidSnapshot = errors.New("invalid source capture snapshot")
	ErrClosed          = errors.New("source capture store is closed")
	ErrNotSealed       = errors.New("source capture store is not sealed")
)

// ConflictError quarantines a document whose admitted source evidence disagrees.
type ConflictError struct {
	document string
	reason   string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("source-revision-conflict: %q: %s", e.document, e.reason)
}

func (e *ConflictError) Document() string { return e.document }
func (*ConflictError) Code() string       { return "source-revision-conflict" }

// CaptureError preserves a backing failure together with its canonical identity.
type CaptureError struct {
	document string
	cause    error
}

func (e *CaptureError) Error() string {
	return fmt.Sprintf("%s: %q: %v", e.Code(), e.document, e.cause)
}

func (e *CaptureError) Document() string { return e.document }
func (e *CaptureError) Code() string {
	if errors.Is(e.cause, context.Canceled) || errors.Is(e.cause, context.DeadlineExceeded) {
		return "cancelled"
	}
	return "capture-failed"
}
func (e *CaptureError) Unwrap() error { return e.cause }

// ClosedError rejects work started after Close begins.
type ClosedError struct{}

func (*ClosedError) Error() string { return ErrClosed.Error() }
func (*ClosedError) Unwrap() error { return ErrClosed }

// JoinError means work remains unjoined. Sources cannot be published until a
// subsequent Close succeeds. Its cause is the Close caller's cancellation.
type JoinError struct{ cause error }

func (e *JoinError) Error() string { return fmt.Sprintf("source capture join failed: %v", e.cause) }
func (e *JoinError) Unwrap() error { return e.cause }
