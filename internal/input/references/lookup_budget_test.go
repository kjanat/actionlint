package references

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestLookupBudgetExcludesWaits(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, stop := StartLookupBudget(t.Context(), 30*time.Second)
		defer stop()
		time.Sleep(10 * time.Second)
		err := WaitOutsideLookupBudget(ctx, func() error {
			return WaitOutsideLookupBudget(ctx, func() error {
				time.Sleep(time.Minute)
				return nil
			})
		})
		if err != nil || ctx.Err() != nil {
			t.Fatalf("wait charged the budget: %v,%v", err, ctx.Err())
		}
		time.Sleep(19 * time.Second)
		if ctx.Err() != nil {
			t.Fatalf("budget expired after 29 seconds of work: %v", context.Cause(ctx))
		}
		time.Sleep(2 * time.Second)
		if !errors.Is(context.Cause(ctx), ErrLookupBudgetExhausted) {
			t.Fatalf("budget did not expire after 31 seconds of work: %v", context.Cause(ctx))
		}
	})
}

func TestLookupBudgetStopAndParentCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		parent, cancel := context.WithCancel(t.Context())
		ctx, stop := StartLookupBudget(parent, time.Second)
		defer stop()
		cancel()
		if !errors.Is(ctx.Err(), context.Canceled) || errors.Is(context.Cause(ctx), ErrLookupBudgetExhausted) {
			t.Fatalf("parent cancellation=%v", context.Cause(ctx))
		}
		stopped, stopNow := StartLookupBudget(t.Context(), time.Second)
		stopNow()
		time.Sleep(2 * time.Second)
		if errors.Is(context.Cause(stopped), ErrLookupBudgetExhausted) {
			t.Fatal("stopped budget expired")
		}
		exhausted, stopLater := StartLookupBudget(t.Context(), time.Second)
		defer stopLater()
		time.Sleep(500 * time.Millisecond)
		if err := WaitOutsideLookupBudget(exhausted, func() error {
			time.Sleep(time.Hour)
			return nil
		}); err != nil || exhausted.Err() != nil {
			t.Fatalf("wait=%v budget=%v", err, context.Cause(exhausted))
		}
		time.Sleep(600 * time.Millisecond)
		if !errors.Is(context.Cause(exhausted), ErrLookupBudgetExhausted) {
			t.Fatalf("budget resumed after a wait did not expire: %v", context.Cause(exhausted))
		}
	})
}

func TestWaitOutsideLookupBudgetWithoutBudget(t *testing.T) {
	t.Parallel()
	want := errors.New("wait failed")
	if err := WaitOutsideLookupBudget(t.Context(), func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}
