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

func TestLookupBudgetCancellationDuringWait(t *testing.T) {
	t.Parallel()
	for _, cancelParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "parent"}[cancelParent], func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				parent, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx, stop := StartLookupBudget(parent, time.Minute)
				defer stop()
				budget := ctx.Value(lookupBudgetKey{}).(*lookupBudget)
				err := WaitOutsideLookupBudget(ctx, func() error {
					if cancelParent {
						cancel()
					} else {
						stop()
					}
					synctest.Wait()
					return ctx.Err()
				})
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("wait returned %v, want cancellation", err)
				}
				budget.mu.Lock()
				active := budget.timer.Stop()
				budget.mu.Unlock()
				if active {
					t.Fatal("returning from the cancelled wait restarted the timer")
				}
			})
		})
	}
}

func TestLookupBudgetIgnoresStoppedTimerCallback(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, stop := StartLookupBudget(t.Context(), 30*time.Second)
		defer stop()
		budget := ctx.Value(lookupBudgetKey{}).(*lookupBudget)
		generation := budget.generation
		time.Sleep(10 * time.Second)
		budget.pause()
		// Timer.Stop cannot recall a callback that has already started.
		budget.expire(generation)
		if ctx.Err() != nil {
			t.Fatalf("stopped timer cancelled a paused lookup: %v", context.Cause(ctx))
		}
		time.Sleep(time.Hour)
		budget.resume()
		budget.expire(generation)
		if ctx.Err() != nil {
			t.Fatalf("old timer cancelled a resumed lookup: %v", context.Cause(ctx))
		}
		time.Sleep(21 * time.Second)
		if !errors.Is(context.Cause(ctx), ErrLookupBudgetExhausted) {
			t.Fatalf("current timer did not exhaust the remaining budget: %v", context.Cause(ctx))
		}
	})
}

func TestLookupBudgetWaitAfterExpiryDoesNotRestartTimer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, stop := StartLookupBudget(t.Context(), time.Second)
		defer stop()
		time.Sleep(2 * time.Second)
		if err := WaitOutsideLookupBudget(ctx, func() error { return ctx.Err() }); !errors.Is(err, context.Canceled) {
			t.Fatalf("expired lookup returned %v", err)
		}
		budget := ctx.Value(lookupBudgetKey{}).(*lookupBudget)
		budget.mu.Lock()
		active := budget.timer.Stop()
		budget.mu.Unlock()
		if active {
			t.Fatal("wait restarted an expired timer")
		}
		if !errors.Is(context.Cause(ctx), ErrLookupBudgetExhausted) {
			t.Fatalf("expiry cause changed: %v", context.Cause(ctx))
		}
	})
}

func TestLookupBudgetExpiresBeforeWaitWithDelayedCallback(t *testing.T) {
	t.Parallel()
	for _, elapsed := range []time.Duration{time.Second, 2 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx, stop := StartLookupBudget(t.Context(), time.Second)
				defer stop()
				budget := ctx.Value(lookupBudgetKey{}).(*lookupBudget)
				budget.timer.Stop()
				generation := budget.generation
				time.Sleep(elapsed)
				err := WaitOutsideLookupBudget(ctx, func() error {
					if !errors.Is(context.Cause(ctx), ErrLookupBudgetExhausted) {
						t.Errorf("wait began with cancellation cause %v, want exhausted budget", context.Cause(ctx))
					}
					budget.expire(generation)
					return ctx.Err()
				})
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("wait returned %v, want cancellation", err)
				}
			})
		})
	}
}

func TestLookupBudgetResumeBeforeCancellationCleanup(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		budget := &lookupBudget{remaining: time.Minute, cancel: cancel}
		budget.arm()
		ctx = context.WithValue(ctx, lookupBudgetKey{}, budget)
		cause := errors.New("parent stopped")
		defer budget.stop(context.Canceled)
		if err := WaitOutsideLookupBudget(ctx, func() error {
			cancel(cause)
			return ctx.Err()
		}); !errors.Is(err, context.Canceled) {
			t.Fatalf("wait returned %v, want cancellation", err)
		}
		// Run the parent-cancellation cleanup after the wait has returned.
		budget.stop(context.Cause(ctx))
		time.Sleep(2 * time.Minute)
		if !errors.Is(context.Cause(ctx), cause) || !budget.stopped || budget.timer.Stop() {
			t.Fatalf("cleanup changed the cause or left a timer running: cause=%v stopped=%v", context.Cause(ctx), budget.stopped)
		}
	})
}

func TestLookupBudgetOverlappingWaits(t *testing.T) {
	t.Parallel()
	for _, first := range []int{0, 1} {
		for _, cancelLast := range []bool{false, true} {
			synctest.Test(t, func(t *testing.T) {
				ctx, stop := StartLookupBudget(t.Context(), 30*time.Second)
				defer stop()
				time.Sleep(10 * time.Second)
				release := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
				done := [2]chan error{make(chan error), make(chan error)}
				for i := range release {
					go func() {
						done[i] <- WaitOutsideLookupBudget(ctx, func() error {
							<-release[i]
							return nil
						})
					}()
				}
				synctest.Wait()
				close(release[first])
				if err := <-done[first]; err != nil {
					t.Fatal(err)
				}
				time.Sleep(time.Minute)
				if ctx.Err() != nil {
					t.Fatalf("first wait %d resumed the timer while the other still waited: %v", first, context.Cause(ctx))
				}
				if cancelLast {
					go stop()
				}
				close(release[1-first])
				if err := <-done[1-first]; err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if cancelLast {
					budget := ctx.Value(lookupBudgetKey{}).(*lookupBudget)
					if !errors.Is(context.Cause(ctx), context.Canceled) || budget.timer.Stop() {
						t.Fatal("cancellation racing with the last return left an active timer")
					}
					return
				}
				time.Sleep(19 * time.Second)
				if ctx.Err() != nil {
					t.Fatalf("last return charged waiting time: %v", context.Cause(ctx))
				}
				time.Sleep(2 * time.Second)
				if !errors.Is(context.Cause(ctx), ErrLookupBudgetExhausted) {
					t.Fatalf("last return did not restart the remaining budget: %v", context.Cause(ctx))
				}
			})
		}
	}
}
