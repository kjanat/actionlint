package references

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrLookupBudgetExhausted = errors.New("reference lookup exceeded its time budget")

type lookupBudget struct {
	mu        sync.Mutex
	timer     *time.Timer
	remaining time.Duration
	resumed   time.Time
	waiting   int
}

type lookupBudgetKey struct{}

func StartLookupBudget(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	budget := &lookupBudget{remaining: limit, resumed: time.Now()}
	budget.timer = time.AfterFunc(limit, func() { cancel(ErrLookupBudgetExhausted) })
	return context.WithValue(ctx, lookupBudgetKey{}, budget), func() {
		budget.timer.Stop()
		cancel(context.Canceled)
	}
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
		b.remaining -= time.Since(b.resumed)
	}
	b.waiting++
}

func (b *lookupBudget) resume() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.waiting--
	if b.waiting == 0 {
		b.resumed = time.Now()
		b.timer.Reset(max(b.remaining, 0))
	}
}
