package component

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"
)

func TestQueuedCallKeepsExecutionBudget(t *testing.T) {
	p := &Plugin{path: "queued.wasm", limits: Limits{Timeout: time.Millisecond}, gate: semaphore.NewWeighted(1)}
	if err := p.gate.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := p.callInterface(t.Context(), RunnerInterface, "resolve")
		result <- err
	}()
	select {
	case err := <-result:
		p.gate.Release(1)
		t.Fatalf("queued call consumed execution budget: %v", err)
	case <-time.After(50 * time.Millisecond):
		p.gate.Release(1)
	}
	if err := <-result; err == nil || !strings.Contains(err.Error(), "is closed") {
		t.Fatalf("admitted call did not reach runtime: %v", err)
	}
}

func TestQueuedCallHonorsCallerCancellation(t *testing.T) {
	p := &Plugin{limits: Limits{Timeout: time.Second}, gate: semaphore.NewWeighted(1)}
	if err := p.gate.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	defer p.gate.Release(1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := p.callInterface(ctx, RunnerInterface, "resolve")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation = %v", err)
	}
}
