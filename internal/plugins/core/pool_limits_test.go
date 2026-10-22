package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/analyzer"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func pooledEngine(t *testing.T) *Engine {
	t.Helper()
	engine, err := Load(t.Context(), filepath.Join("testdata", "engines", "echo.wasm"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close(context.Background()) })
	for _, instance := range engine.idle {
		if err := instance.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	engine.idle = nil
	return engine
}

type gatedRuntime struct {
	wazero.Runtime
	entered chan struct{}
	release <-chan struct{}
}

func (r gatedRuntime) InstantiateModule(ctx context.Context, compiled wazero.CompiledModule, config wazero.ModuleConfig) (api.Module, error) {
	module, err := r.Runtime.InstantiateModule(ctx, compiled, config)
	if err != nil {
		return nil, err
	}
	return gatedModule{Module: module, entered: r.entered, release: r.release}, nil
}

type gatedModule struct {
	api.Module
	entered chan struct{}
	release <-chan struct{}
}

func (m gatedModule) ExportedFunction(name string) api.Function {
	function := m.Module.ExportedFunction(name)
	if name == "check" {
		return gatedFunction{Function: function, entered: m.entered, release: m.release}
	}
	return function
}

type gatedFunction struct {
	api.Function
	entered chan struct{}
	release <-chan struct{}
}

func (f gatedFunction) Call(ctx context.Context, params ...uint64) ([]uint64, error) {
	select {
	case f.entered <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-f.release:
		return f.Function.Call(ctx, params...)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestConcurrentChecksBoundActiveAndRetainedInstances(t *testing.T) {
	engine := pooledEngine(t)
	entered := make(chan struct{}, 24)
	release := make(chan struct{})
	engine.runtime = gatedRuntime{Runtime: engine.runtime, entered: entered, release: release}
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	errs := make(chan error, 24)
	var calls sync.WaitGroup
	for range 24 {
		calls.Go(func() {
			_, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "print(missing)", Filename: "step.py"})
			errs <- err
		})
	}
	for range 4 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("four concurrent checks did not start")
		}
	}
	select {
	case <-entered:
		t.Error("a fifth guest executed while four instances were busy")
	case <-time.After(50 * time.Millisecond):
	}
	ctx, cancel := context.WithCancel(t.Context())
	queued := make(chan error, 1)
	go func() {
		_, err := engine.Check(ctx, analyzer.ScriptRequest{Source: "print(missing)", Filename: "cancelled.py"})
		queued <- err
	}()
	cancel()
	select {
	case err := <-queued:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled queued call: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("queued call ignored cancellation")
	}
	unblock.Do(func() { close(release) })
	calls.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if len(engine.idle) > 4 {
		t.Fatalf("retained %d instances after concurrent checks", len(engine.idle))
	}
}

func TestCallTimeoutIncludesGuestInstantiation(t *testing.T) {
	engine := pooledEngine(t)
	// This module's start function loops forever.
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 4, 1, 96, 0, 0, 3, 2, 1, 0, 8, 1, 0, 10, 9, 1, 7, 0, 3, 64, 12, 0, 11, 11}
	compiled, err := engine.runtime.CompileModule(t.Context(), module)
	if err != nil {
		t.Fatal(err)
	}
	engine.compiled = compiled
	engine.limits.Timeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	start := time.Now()
	_, err = engine.Check(ctx, analyzer.ScriptRequest{Source: "x", Filename: "step.py"})
	if err == nil || !strings.Contains(err.Error(), "exceeded its time limit") {
		t.Fatalf("initializer escaped the call limit: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 700*time.Millisecond {
		t.Fatalf("initializer used caller's 1s deadline instead of 50ms engine limit: %s", elapsed)
	}
}
