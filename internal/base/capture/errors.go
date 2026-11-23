package capture

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
)

var (
	ErrInvalidSnapshot = errors.New("invalid source capture snapshot")
	ErrClosed          = errors.New("source capture store is closed")
	ErrNotSealed       = errors.New("source capture store is not sealed")
)

type ConflictError struct {
	document string
	reason   string
}

func (e ConflictError) Error() string {
	return fmt.Sprintf("source-revision-conflict: %q: %s", e.document, e.reason)
}

func (e ConflictError) Document() string { return e.document }
func (ConflictError) Code() string       { return "source-revision-conflict" }

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

type ReadError struct {
	context string
	err     error
}

// NewReadError requires a non-nil err.
func NewReadError(context string, err error) *ReadError {
	return &ReadError{context: context, err: err}
}

func (e *ReadError) Error() string {
	shown := e.err
	if captured, ok := errors.AsType[*CaptureError](e.err); ok {
		shown = captured.cause
	}
	return e.context + ": " + shown.Error()
}

func (e *ReadError) Unwrap() error { return e.err }

type ClosedError struct{}

func (*ClosedError) Error() string { return ErrClosed.Error() }
func (*ClosedError) Unwrap() error { return ErrClosed }

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

func (e *InitError) Pending() bool { return e.pending != nil && !e.joined.Load() }

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

type JoinError struct{ cause error }

func (e *JoinError) Error() string { return fmt.Sprintf("source capture join failed: %v", e.cause) }
func (e *JoinError) Unwrap() error { return e.cause }
