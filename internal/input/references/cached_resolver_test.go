package references_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/repository"
)

type resolverFunc func(context.Context, references.Request) (references.ResolutionRecord, error)

func (f resolverFunc) Resolve(ctx context.Context, request references.Request) (references.ResolutionRecord, error) {
	return f(ctx, request)
}

func knownRepositoryID(tb testing.TB, value uint64) repository.ID {
	tb.Helper()
	id, err := repository.KnownID(value)
	if err != nil {
		tb.Fatal(err)
	}
	return id
}

func TestCachedResolverCachesExactRequests(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	session := references.NewCachedResolver(resolverFunc(func(context.Context, references.Request) (references.ResolutionRecord, error) {
		calls.Add(1)
		return references.ResolutionRecord{FetchLog: json.RawMessage(`{"source":"host"}`)}, nil
	}))
	request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action"}
	result, err := session.Resolve(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(result.FetchLog) {
		t.Fatalf("result=%s", result.FetchLog)
	}
	cached, err := session.Resolve(t.Context(), request)
	if err != nil || !json.Valid(cached.FetchLog) || calls.Load() != 1 {
		t.Fatalf("cached=%s calls=%d err=%v", cached.FetchLog, calls.Load(), err)
	}
	request.Kind = "workflow"
	if _, err := session.Resolve(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	request.ExpectedRepositoryID = knownRepositoryID(t, 7)
	if _, err := session.Resolve(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("requests with different consumer or identity shared a cache entry: %d", calls.Load())
	}
}

func TestCachedResolverCoalescesAndCancelsWaiter(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	session := references.NewCachedResolver(resolverFunc(func(context.Context, references.Request) (references.ResolutionRecord, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return references.ResolutionRecord{RepositoryID: knownRepositoryID(t, 7)}, nil
	}))
	request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action"}
	first := make(chan error, 1)
	go func() { _, err := session.Resolve(t.Context(), request); first <- err }()
	<-started
	waiter, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := session.Resolve(waiter, request)
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error = %v", err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	result, err := session.Resolve(t.Context(), request)
	if err != nil || result.RepositoryID != knownRepositoryID(t, 7) || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d error=%v", result, calls.Load(), err)
	}
}

func TestCachedResolverWaiterRetriesAbandonedResolution(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64
		session := references.NewCachedResolver(resolverFunc(func(ctx context.Context, _ references.Request) (references.ResolutionRecord, error) {
			if calls.Add(1) == 1 {
				<-ctx.Done()
				return references.ResolutionRecord{}, ctx.Err()
			}
			return references.ResolutionRecord{RepositoryID: knownRepositoryID(t, 7)}, nil
		}))
		request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action"}
		initiator, cancel := context.WithCancel(t.Context())
		first := make(chan error, 1)
		go func() { _, err := session.Resolve(initiator, request); first <- err }()
		synctest.Wait()
		waiter, stop := network.StartLookupBudget(t.Context(), time.Second)
		defer stop()
		second := make(chan error, 1)
		var result references.ResolutionRecord
		go func() {
			var err error
			result, err = session.Resolve(waiter, request)
			second <- err
		}()
		synctest.Wait()
		time.Sleep(time.Minute)
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Fatalf("initiator error = %v", err)
		}
		if err := <-second; err != nil || result.RepositoryID != knownRepositoryID(t, 7) || calls.Load() != 2 || waiter.Err() != nil {
			t.Fatalf("waiter result=%+v error=%v calls=%d budget=%v", result, err, calls.Load(), context.Cause(waiter))
		}
	})
}
