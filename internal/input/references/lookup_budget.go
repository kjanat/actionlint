package references

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrLookupBudgetExhausted = errors.New("reference lookup exceeded its time budget")

type lookupBudget struct {
	mu         sync.Mutex
	timer      *time.Timer
	remaining  time.Duration
	resumed    time.Time
	waiting    int
	generation uint64
	stopped    bool
	cancel     context.CancelCauseFunc
}

type lookupBudgetKey struct{}

func StartLookupBudget(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	budget := &lookupBudget{remaining: limit, cancel: cancel}
	budget.mu.Lock()
	budget.arm()
	budget.mu.Unlock()
	context.AfterFunc(ctx, func() { budget.stop(context.Cause(ctx)) })
	return context.WithValue(ctx, lookupBudgetKey{}, budget), func() {
		budget.stop(context.Canceled)
	}
}

func (b *lookupBudget) stop(cause error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	b.stopped = true
	b.timer.Stop()
	b.cancel(cause)
}

// arm starts a timer for the remaining lookup time. The caller must hold mu.
func (b *lookupBudget) arm() {
	b.resumed = time.Now()
	b.generation++
	generation := b.generation
	b.timer = time.AfterFunc(max(b.remaining, 0), func() { b.expire(generation) })
}

func (b *lookupBudget) expire(generation uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped || b.waiting != 0 || generation != b.generation {
		return
	}
	b.stopped = true
	b.cancel(ErrLookupBudgetExhausted)
}

func WaitOutsideLookupBudget(ctx context.Context, wait func() error) error {
	budget, _ := ctx.Value(lookupBudgetKey{}).(*lookupBudget)
	if budget == nil {
		return wait()
	}
	budget.pause()
	defer budget.resume()
	return wait()
}

func (b *lookupBudget) pause() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.waiting == 0 {
		b.timer.Stop()
		b.generation++
		b.remaining -= time.Since(b.resumed)
		if b.remaining <= 0 && !b.stopped {
			b.stopped = true
			b.cancel(ErrLookupBudgetExhausted)
		}
	}
	b.waiting++
}

func (b *lookupBudget) resume() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.waiting--
	if b.waiting == 0 && !b.stopped {
		b.arm()
	}
}
