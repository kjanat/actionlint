package component

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTimeoutFailurePreservesCause(t *testing.T) {
	p := &Plugin{path: "timeout.wasm", limits: Limits{Timeout: time.Second}}
	cause := errors.New("guest interrupted")
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	for _, test := range []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"caller deadline", ctx, cause},
		{"returned deadline", t.Context(), errors.Join(context.DeadlineExceeded, cause)},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := p.failure(test.ctx, test.err)
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, cause) {
				t.Fatalf("timeout lost its identity or cause: %v", err)
			}
			if !strings.Contains(err.Error(), "plugin timeout.wasm exceeded its time limit") {
				t.Fatalf("timeout lost plugin context: %v", err)
			}
		})
	}
}

func TestCancellationFailurePreservesCause(t *testing.T) {
	p := &Plugin{path: "cancelled.wasm"}
	cause := errors.New("guest interrupted")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"caller cancellation", ctx, cause},
		{"returned cancellation", t.Context(), errors.Join(context.Canceled, cause)},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := p.failure(test.ctx, test.err)
			if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
				t.Fatalf("cancellation lost its identity or cause: %v", err)
			}
		})
	}
}
