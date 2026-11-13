package component

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kjanat/wacogo"
	"github.com/kjanat/wazero"
	"github.com/kjanat/wazero/experimental"
)

const compilationWorkerMarker = -173

// Wazevo reads its public worker setting when it enters cold module compilation.
// A negative setting selects one worker and lets this context count those reads.
// The interpreter does not read this setting.
type compilationCountingContext struct {
	context.Context
	compilations atomic.Int64
}

func (c *compilationCountingContext) Value(key any) any {
	value := c.Context.Value(key)
	if workers, ok := value.(int); ok && workers == compilationWorkerMarker {
		c.compilations.Add(1)
	}
	return value
}

func newCompilationTest(t *testing.T) (*componentCompilation, *compilationCountingContext, []byte) {
	t.Helper()
	compilation := newComponentCompilation()
	t.Cleanup(func() {
		if err := compilation.cache.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	ctx := &compilationCountingContext{Context: experimental.WithCompilationWorkers(t.Context(), compilationWorkerMarker)}
	data, err := os.ReadFile(filepath.Join("testdata", "wat", "aggregate-memory.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	return compilation, ctx, data
}

func compilationTestEngine(t *testing.T, compilation *componentCompilation, ctx context.Context) *wacogo.Engine {
	t.Helper()
	engine := compilation.newEngine(ctx, Limits{MemoryPages: DefaultMemoryPages})
	t.Cleanup(func() {
		if err := engine.Close(context.WithoutCancel(ctx)); err != nil {
			t.Error(err)
		}
	})
	return engine
}

func compilationTestInterpreterEngine(t *testing.T, compilation *componentCompilation, ctx context.Context) *wacogo.Engine {
	t.Helper()
	engine := wacogo.NewEngine(ctx, wacogo.WithRuntimeConfig(wazero.NewRuntimeConfigInterpreter().WithCompilationCache(compilation.cache)))
	t.Cleanup(func() {
		if err := engine.Close(context.WithoutCancel(ctx)); err != nil {
			t.Error(err)
		}
	})
	return engine
}

func compilationTestGrow(t *testing.T, ctx context.Context, instance *wacogo.ComponentInstance, expected uint32) {
	t.Helper()
	grow := instance.ExportedFunc("grow")
	if grow == nil {
		t.Fatal("fixture does not export grow")
	}
	values, err := grow.Call(ctx)
	if err != nil || len(values) != 1 || values[0] != wacogo.ValU32(expected) {
		t.Fatalf("memory.grow returned %v, error=%v; want previous size %d", values, err, expected)
	}
}

func TestComponentCompilationCacheSurvivesRuntimeClose(t *testing.T) {
	t.Parallel()
	t.Run("default runtime", func(t *testing.T) {
		t.Parallel()
		testComponentCompilationRuntimeClose(t, compilationTestEngine)
	})
	t.Run("interpreter", func(t *testing.T) {
		t.Parallel()
		testComponentCompilationRuntimeClose(t, compilationTestInterpreterEngine)
	})
}

func testComponentCompilationRuntimeClose(t *testing.T, newEngine func(*testing.T, *componentCompilation, context.Context) *wacogo.Engine) {
	t.Helper()
	compilation, ctx, data := newCompilationTest(t)
	firstEngine := newEngine(t, compilation, ctx)
	first, err := compilation.load(ctx, firstEngine, data)
	if err != nil {
		t.Fatal(err)
	}
	firstGuest, err := first.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	compilationTestGrow(t, ctx, firstGuest, 1)
	compilationTestGrow(t, ctx, firstGuest, 2)
	if err := firstGuest.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := firstEngine.Close(ctx); err != nil {
		t.Fatal(err)
	}
	compiled := ctx.compilations.Load()
	secondEngine := newEngine(t, compilation, ctx)
	second, err := compilation.load(ctx, secondEngine, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("compiler reuses compiled modules", func(t *testing.T) {
		if compiled == 0 {
			t.Skip("runtime does not expose the compiler worker setting")
		}
		if got := ctx.compilations.Load(); got != compiled {
			t.Fatalf("loading after runtime close compiled modules again: before=%d after=%d", compiled, got)
		}
	})
	secondGuest, err := second.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := secondGuest.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	compilationTestGrow(t, ctx, secondGuest, 1)
}

func TestComponentCompilationCacheConcurrentLoads(t *testing.T) {
	t.Parallel()
	compilation, ctx, data := newCompilationTest(t)
	const loaders = 4
	engines := make([]*wacogo.Engine, loaders)
	for i := range engines {
		engines[i] = compilationTestEngine(t, compilation, ctx)
	}
	start := make(chan struct{})
	results := make(chan error, loaders)
	for _, engine := range engines {
		go func() {
			<-start
			_, err := compilation.load(ctx, engine, data)
			results <- err
		}()
	}
	close(start)
	for range loaders {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	t.Run("compiler compiles once", func(t *testing.T) {
		t.Parallel()
		got := ctx.compilations.Load()
		if got == 0 {
			t.Skip("runtime does not expose the compiler worker setting")
		}
		if got != 1 {
			t.Fatalf("concurrent loads entered cold compilation %d times; want once", got)
		}
	})
}

func TestComponentCompilationCacheCancelledWaiter(t *testing.T) {
	t.Parallel()
	compilation, ctx, data := newCompilationTest(t)
	engine := compilationTestEngine(t, compilation, ctx)
	if err := compilation.gate.Acquire(ctx, 1); err != nil {
		t.Fatal(err)
	}
	waiter, cancel := context.WithCancel(ctx)
	cancel()
	_, err := compilation.load(waiter, engine, data)
	compilation.gate.Release(1)
	if !errors.Is(err, context.Canceled) || ctx.compilations.Load() != 0 {
		t.Fatalf("cancelled load error=%v, cold compilations=%d", err, ctx.compilations.Load())
	}
	if _, err := compilation.load(ctx, engine, data); err != nil {
		t.Fatalf("cancelled waiter blocked the next load: %v", err)
	}
}

func TestComponentCompilationCacheFailedLoadReleasesGate(t *testing.T) {
	t.Parallel()
	compilation, ctx, data := newCompilationTest(t)
	engine := compilationTestEngine(t, compilation, ctx)
	if _, err := compilation.load(ctx, engine, []byte("invalid component")); err == nil {
		t.Fatal("loaded invalid component")
	}
	retry, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := compilation.load(retry, engine, data); err != nil {
		t.Fatalf("failed load blocked the next load: %v", err)
	}
}
