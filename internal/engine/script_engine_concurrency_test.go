package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/analyzer"
)

type concurrentEngine struct {
	closes  atomic.Int32
	onClose func(context.Context) error
}

func (*concurrentEngine) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	return nil, nil
}

func (e *concurrentEngine) Close(ctx context.Context) error {
	e.closes.Add(1)
	if e.onClose != nil {
		return e.onClose(ctx)
	}
	return nil
}

type concurrentLoadResult struct {
	backend analyzer.ScriptBackend
	err     error
}

func loadScriptAsync(engines *ScriptEngines, ctx context.Context, path string) <-chan concurrentLoadResult {
	result := make(chan concurrentLoadResult, 1)
	go func() {
		backend, err := engines.Load(ctx, path)
		result <- concurrentLoadResult{backend, err}
	}()
	return result
}

func scriptResultWithin(t *testing.T, result <-chan concurrentLoadResult, operation string) concurrentLoadResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(2 * time.Second):
		t.Fatalf("%s blocked behind another callback", operation)
		return concurrentLoadResult{}
	}
}

func scriptSignalWithin(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not start", operation)
	}
}

func TestScriptEnginesLoadIndependentPathsConcurrently(t *testing.T) {
	entered, blocked := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(blocked) })
	defer release()
	engines := NewScriptEngines(func(_ context.Context, path string) (analyzer.ScriptBackend, error) {
		if path == "slow.wasm" {
			close(entered)
			<-blocked
		}
		return &concurrentEngine{}, nil
	})
	slow := loadScriptAsync(engines, t.Context(), "slow.wasm")
	scriptSignalWithin(t, entered, "slow load")
	fast := scriptResultWithin(t, loadScriptAsync(engines, t.Context(), "fast.wasm"), "independent load")
	if fast.backend == nil || fast.err != nil {
		t.Fatalf("independent load failed: %+v", fast)
	}
	release()
	if got := scriptResultWithin(t, slow, "slow load completion"); got.backend == nil || got.err != nil {
		t.Fatalf("slow load failed: %+v", got)
	}
	if err := engines.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestScriptEnginesCanceledWaiterPreservesSharedLoad(t *testing.T) {
	entered, blocked := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(blocked) })
	defer release()
	backend := &concurrentEngine{}
	var loads atomic.Int32
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		loads.Add(1)
		close(entered)
		<-blocked
		return backend, nil
	})
	first := loadScriptAsync(engines, t.Context(), "same.wasm")
	scriptSignalWithin(t, entered, "first load")
	waiting := loadScriptAsync(engines, t.Context(), "same.wasm")
	ctx, cancel := context.WithCancel(t.Context())
	canceled := loadScriptAsync(engines, ctx, "same.wasm")
	cancel()
	if got := scriptResultWithin(t, canceled, "canceled waiter"); got.backend != nil || !errors.Is(got.err, context.Canceled) {
		t.Fatalf("canceled waiter=%+v", got)
	}
	release()
	for _, result := range []<-chan concurrentLoadResult{first, waiting} {
		if got := scriptResultWithin(t, result, "shared load"); got.backend != backend || got.err != nil {
			t.Fatalf("shared load=%+v", got)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("same path loaded %d times", loads.Load())
	}
	if err := engines.Close(t.Context()); err != nil || backend.closes.Load() != 1 {
		t.Fatalf("close=%v calls=%d", err, backend.closes.Load())
	}
}

func TestScriptEnginesCloseDiscardsLateLoad(t *testing.T) {
	entered, blocked := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(blocked) })
	defer release()
	cleanup := make(chan error, 1)
	backend := &concurrentEngine{onClose: func(ctx context.Context) error { cleanup <- ctx.Err(); return nil }}
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		close(entered)
		<-blocked
		return backend, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	loading := loadScriptAsync(engines, ctx, "late.wasm")
	scriptSignalWithin(t, entered, "late load")
	waiting := loadScriptAsync(engines, t.Context(), "late.wasm")
	closing := make(chan concurrentLoadResult, 1)
	go func() { closing <- concurrentLoadResult{err: engines.Close(t.Context())} }()
	if got := scriptResultWithin(t, closing, "close during load"); got.err != nil {
		t.Fatal(got.err)
	}
	if got := scriptResultWithin(t, waiting, "pending waiter after close"); got.backend != nil || !errors.Is(got.err, errScriptEnginesClosed) {
		t.Fatalf("pending waiter survived session close: %+v", got)
	}
	if got := scriptResultWithin(t, loadScriptAsync(engines, t.Context(), "late.wasm"), "closed waiter"); got.backend != nil || got.err == nil || !strings.Contains(got.err.Error(), "closed") {
		t.Fatalf("closed session reused a pending load: %+v", got)
	}
	cancel()
	release()
	if got := scriptResultWithin(t, loading, "late result cleanup"); got.backend != nil || got.err == nil || !strings.Contains(got.err.Error(), "closed") {
		t.Fatalf("late result resurrected session: %+v", got)
	}
	if err := <-cleanup; err != nil {
		t.Fatalf("late cleanup inherited canceled context: %v", err)
	}
	if err := engines.Close(t.Context()); err != nil || backend.closes.Load() != 1 {
		t.Fatalf("close=%v calls=%d", err, backend.closes.Load())
	}
}

func TestScriptEnginesLateLoadPreservesCleanupErrors(t *testing.T) {
	entered, blocked := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(blocked) })
	defer release()
	loadErr, closeErr := errors.New("load failed"), errors.New("cleanup failed")
	backend := &concurrentEngine{onClose: func(context.Context) error { return closeErr }}
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		close(entered)
		<-blocked
		return backend, loadErr
	})
	loading := loadScriptAsync(engines, t.Context(), "late.wasm")
	scriptSignalWithin(t, entered, "late failed load")
	if err := engines.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	release()
	got := scriptResultWithin(t, loading, "late failed load cleanup")
	if got.backend != nil || !errors.Is(got.err, errScriptEnginesClosed) || !errors.Is(got.err, loadErr) || !errors.Is(got.err, closeErr) {
		t.Fatalf("late load discarded errors: %+v", got)
	}
	if backend.closes.Load() != 1 {
		t.Fatalf("late backend closed %d times", backend.closes.Load())
	}
}

func TestScriptEnginesConcurrentFailureIsCached(t *testing.T) {
	entered, blocked := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(blocked) })
	defer release()
	loadErr := errors.New("load failed")
	var loads atomic.Int32
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		loads.Add(1)
		close(entered)
		<-blocked
		return nil, loadErr
	})
	first := loadScriptAsync(engines, t.Context(), "failed.wasm")
	scriptSignalWithin(t, entered, "failed load")
	waiting := loadScriptAsync(engines, t.Context(), "failed.wasm")
	release()
	for _, result := range []<-chan concurrentLoadResult{first, waiting} {
		if got := scriptResultWithin(t, result, "shared failed load"); got.backend != nil || !errors.Is(got.err, loadErr) {
			t.Fatalf("failed load=%+v", got)
		}
	}
	if backend, err := engines.Load(t.Context(), "failed.wasm"); backend != nil || !errors.Is(err, loadErr) {
		t.Fatalf("cached failure=%v, %v", backend, err)
	}
	if loads.Load() != 1 {
		t.Fatalf("failed path loaded %d times", loads.Load())
	}
}

func TestScriptEnginesCloseCallbacksReleaseCacheLock(t *testing.T) {
	entered, blocked := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(blocked) })
	defer release()
	backend := &concurrentEngine{onClose: func(context.Context) error { close(entered); <-blocked; return nil }}
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) { return backend, nil })
	if _, err := engines.Load(t.Context(), "loaded.wasm"); err != nil {
		t.Fatal(err)
	}
	closing := make(chan concurrentLoadResult, 1)
	go func() { closing <- concurrentLoadResult{err: engines.Close(t.Context())} }()
	scriptSignalWithin(t, entered, "backend close")
	if got := scriptResultWithin(t, loadScriptAsync(engines, t.Context(), "other.wasm"), "load while backend closes"); got.backend != nil || got.err == nil || !strings.Contains(got.err.Error(), "closed") {
		t.Fatalf("closed session load=%+v", got)
	}
	second := make(chan concurrentLoadResult, 1)
	go func() { second <- concurrentLoadResult{err: engines.Close(t.Context())} }()
	if got := scriptResultWithin(t, second, "concurrent close"); got.err != nil {
		t.Fatal(got.err)
	}
	release()
	if got := scriptResultWithin(t, closing, "backend close completion"); got.err != nil || backend.closes.Load() != 1 {
		t.Fatalf("close=%v calls=%d", got.err, backend.closes.Load())
	}
}

func TestScriptEnginesCountsInflightAdmissions(t *testing.T) {
	entered, blocked := make(chan struct{}, maxScriptEngines), make(chan struct{})
	release := sync.OnceFunc(func() { close(blocked) })
	defer release()
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		entered <- struct{}{}
		<-blocked
		return &concurrentEngine{}, nil
	})
	var pending []<-chan concurrentLoadResult
	for i := range maxScriptEngines {
		pending = append(pending, loadScriptAsync(engines, t.Context(), fmt.Sprintf("pending-%d.wasm", i)))
	}
	for range maxScriptEngines {
		scriptSignalWithin(t, entered, "bounded parallel admission")
	}
	got := scriptResultWithin(t, loadScriptAsync(engines, t.Context(), "excess.wasm"), "excess admission")
	if got.backend != nil || got.err == nil || !strings.Contains(got.err.Error(), "session limit of 4") {
		t.Fatalf("pending loads bypassed admission cap: %+v", got)
	}
	release()
	for _, result := range pending {
		if got := scriptResultWithin(t, result, "admitted load"); got.backend == nil || got.err != nil {
			t.Fatalf("admitted load=%+v", got)
		}
	}
	if err := engines.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
