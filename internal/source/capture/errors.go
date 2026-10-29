package capture

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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

// ReadFailure presents a failed read under a user-facing context. A
// *CaptureError is shown by its cause, which already names the file,
// instead of its code and canonical document identity. The error itself
// stays in the chain for errors.As and Code.
type ReadFailure struct {
	Context string
	Err     error
}

func (e *ReadFailure) Error() string {
	shown := e.Err
	var captured *CaptureError
	if errors.As(e.Err, &captured) {
		shown = captured.cause
	}
	return e.Context + ": " + shown.Error()
}

func (e *ReadFailure) Unwrap() error { return e.Err }

// ClosedError rejects work started after Close begins.
type ClosedError struct{}

func (*ClosedError) Error() string { return ErrClosed.Error() }
func (*ClosedError) Unwrap() error { return ErrClosed }

// InitError owns metadata work left behind by a failed New. No partially built
// Store or source data is exposed. Cancellation cannot interrupt an OS metadata
// call: pending work can finish later, but remains unjoined until Join succeeds.
type InitError struct {
	cause   error
	pending <-chan struct{}
	joined  atomic.Bool
}

func (e *InitError) Error() string {
	return fmt.Sprintf("source capture initialization failed: %v", e.cause)
}
func (e *InitError) Unwrap() error { return e.cause }

func (e *InitError) Code() string {
	if errors.Is(e.cause, context.Canceled) || errors.Is(e.cause, context.DeadlineExceeded) {
		return "cancelled"
	}
	return "capture-failed"
}

func (*InitError) Stage() string { return "capture" }

// Pending reports metadata work that the constructor could not join.
func (e *InitError) Pending() bool { return e.pending != nil && !e.joined.Load() }

// Join waits only for the private metadata flight. It never publishes sources.
// A timed-out join leaves ownership here so the caller can retry after release.
func (e *InitError) Join(ctx context.Context) error {
	if ctx == nil {
		return errors.New("source capture initialization join requires a caller context")
	}
	if !e.Pending() {
		return nil
	}
	select {
	case <-e.pending:
	default:
		select {
		case <-e.pending:
		case <-ctx.Done():
			return &JoinError{ctx.Err()}
		}
	}
	e.joined.Store(true)
	return nil
}

// JoinError means work remains unjoined. Sources cannot be published until a
// subsequent Close succeeds. Failed construction instead requires InitError.Join.
// Its cause is the join caller's cancellation.
type JoinError struct{ cause error }

func (e *JoinError) Error() string { return fmt.Sprintf("source capture join failed: %v", e.cause) }
func (e *JoinError) Unwrap() error { return e.cause }
