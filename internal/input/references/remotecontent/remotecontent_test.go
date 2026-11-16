package remotecontent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func newCountingStore(parses *atomic.Int32) *Store[string, string, string] {
	return New[string, string](func(content []byte) string {
		parses.Add(1)
		return "parsed " + string(content)
	})
}

func TestAcquireSharesOneRunningAcquisition(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var parses, acquisitions atomic.Int32
		store := newCountingStore(&parses)
		release := make(chan struct{})
		acquire := func(context.Context) (string, error) {
			acquisitions.Add(1)
			<-release
			return "result", nil
		}
		var group sync.WaitGroup
		results := make([]string, 8)
		for i := range results {
			group.Go(func() { results[i], _ = store.Acquire(t.Context(), "key", acquire) })
		}
		synctest.Wait()
		close(release)
		group.Wait()
		for i, result := range results {
			if result != "result" {
				t.Fatalf("caller %d result = %q", i, result)
			}
		}
		if acquisitions.Load() != 1 {
			t.Fatalf("acquisitions = %d, want 1", acquisitions.Load())
		}
		if result, ok := store.Lookup("key"); !ok || result != "result" {
			t.Fatalf("kept result = %q, %t", result, ok)
		}
	})
}

func TestAcquireSharesFailureWithWaitersAndRetriesLater(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var parses atomic.Int32
		store := newCountingStore(&parses)
		release := make(chan struct{})
		var group sync.WaitGroup
		var owner, waiter error
		group.Go(func() {
			_, owner = store.Acquire(t.Context(), "key", func(context.Context) (string, error) {
				<-release
				return "", errors.New("provider returned another repository")
			})
		})
		synctest.Wait()
		group.Go(func() {
			_, waiter = store.Acquire(t.Context(), "key", func(context.Context) (string, error) {
				t.Error("waiter acquired instead of sharing the running acquisition")
				return "", nil
			})
		})
		synctest.Wait()
		close(release)
		group.Wait()
		if owner == nil || owner.Error() != "provider returned another repository" {
			t.Fatalf("owner failure = %v", owner)
		}
		if waiter == nil || waiter.Error() != owner.Error() {
			t.Fatalf("waiter failure = %v, want %v", waiter, owner)
		}
		if _, ok := store.Lookup("key"); ok {
			t.Fatal("failure was kept")
		}
		if result, err := store.Acquire(t.Context(), "key", func(context.Context) (string, error) { return "later", nil }); err != nil || result != "later" {
			t.Fatalf("later acquisition = %q, %v", result, err)
		}
	})
}

func TestAcquireRetriesForLiveWaiterAfterCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var parses atomic.Int32
		store := newCountingStore(&parses)
		ownerContext, cancelOwner := context.WithCancel(t.Context())
		defer cancelOwner()
		release := make(chan struct{})
		var group sync.WaitGroup
		var ownerErr, waiterErr error
		var waiterResult string
		group.Go(func() {
			_, ownerErr = store.Acquire(ownerContext, "key", func(ctx context.Context) (string, error) {
				<-release
				cancelOwner()
				return "", ctx.Err()
			})
		})
		synctest.Wait()
		group.Go(func() {
			waiterResult, waiterErr = store.Acquire(t.Context(), "key", func(context.Context) (string, error) {
				return "retried", nil
			})
		})
		synctest.Wait()
		close(release)
		group.Wait()
		if !errors.Is(ownerErr, context.Canceled) {
			t.Fatalf("owner error = %v, want cancellation", ownerErr)
		}
		if waiterErr != nil || waiterResult != "retried" {
			t.Fatalf("waiter = %q, %v", waiterResult, waiterErr)
		}
	})
}

func TestAcquireReturnsCallerCancellation(t *testing.T) {
	t.Parallel()
	var parses atomic.Int32
	store := newCountingStore(&parses)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	if _, err := store.Acquire(ctx, "key", func(context.Context) (string, error) {
		called = true
		return "", nil
	}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled acquisition = %v, called %t", err, called)
	}
}

func TestParseOncePerLocationAndRejectChangedContent(t *testing.T) {
	t.Parallel()
	var parses atomic.Int32
	store := newCountingStore(&parses)
	location := Location{Host: "github.com", Repository: "Example/Repo", Commit: "a", Path: "dir"}
	for range 3 {
		if value, ok := store.Parse(Content{Location: location, Name: "action.yml", Bytes: "x"}); !ok || value != "parsed x" {
			t.Fatalf("parse = %q, %t", value, ok)
		}
	}
	location.Repository = "example/repo"
	if value, ok := store.Parse(Content{Location: location, Name: "action.yml", Bytes: "x"}); !ok || value != "parsed x" {
		t.Fatalf("repository case changed the location: %q, %t", value, ok)
	}
	if parses.Load() != 1 {
		t.Fatalf("parses = %d, want 1", parses.Load())
	}
	for _, changed := range []Content{
		{Location: location, Name: "action.yml", Bytes: "y"},
		{Location: location, Name: "action.yaml", Bytes: "x"},
	} {
		if _, ok := store.Parse(changed); ok {
			t.Fatalf("changed content accepted: %+v", changed)
		}
	}
}
