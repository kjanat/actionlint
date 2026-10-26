package component

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourceannotations"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/wasi/cli/stdin"
	"github.com/kjanat/wazero/experimental"
)

func TestCallTeardownPreservesFailuresAndClosesRuntimeLast(t *testing.T) {
	p, err := Load(t.Context(), filepath.Join("testdata", "plugins", "source-annotations.wasm"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	ctx := experimental.WithMemoryAllocator(t.Context(), p.memory)
	teardown := callScope{plugin: p}
	if err := teardown.prepareImports(ctx); err != nil {
		t.Fatal(err)
	}
	input, err := stdin.NewInstance(ctx, p.engine, teardown.world.Streams, teardown.world.Error, teardown.world.Poll, nil)
	if err != nil {
		t.Fatal(err)
	}
	invocations, err := p.invocationFactory.NewInstance(ctx, invocationContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	teardown.invocations = invocations
	annotations, err := p.annotationsFactory.NewInstance(ctx, sourceAnnotations{}, &annotationsbind.Deps{InvocationContext: invocations.Core()})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	teardown.output.log = func(string) { order = append(order, "output") }
	if _, err := teardown.output.Write([]byte("pending line")); err != nil {
		t.Fatal(err)
	}
	outputErr := errors.New("output failure before cancellation")
	teardown.output.err = outputErr
	stdinErr := errors.New("stdin cleanup")
	annotationsErr := errors.New("annotations cleanup")
	hostErr := errors.New("host cleanup")
	input.RegisterResource(teardownDrop{t, "stdin", stdinErr, &order})
	annotations.RegisterResource(teardownDrop{t, "annotations", annotationsErr, &order})
	teardown.host.RegisterResource(teardownDrop{t, "host", hostErr, &order})

	data, err := os.ReadFile(filepath.Join("testdata", "wat", "aggregate-tables.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := p.engine.LoadComponent(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	guestErr := errors.New("guest table refund")
	runtimeErr := errors.New("runtime table refund")
	instantiate := func(name string, failure error) *wacogo.ComponentInstance {
		t.Helper()
		allocation := wacogo.WithCoreTableAllocator(ctx, func([]wacogo.CoreTable) (func(), error) {
			return func() {
				order = append(order, name)
				panic(failure)
			}, nil
		})
		instance, err := compiled.Instantiate(allocation)
		if err != nil {
			t.Fatal(err)
		}
		return instance
	}
	guest := instantiate("guest", guestErr)
	// This instance stays live until engine shutdown.
	instantiate("runtime", runtimeErr)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	primary := errors.New("analysis failed")
	result := errors.Join(primary, context.Canceled)
	teardown.guest, teardown.annotations, teardown.stdin = guest, annotations, input
	teardown.finish(cancelled, &result)
	for _, cause := range []error{primary, context.Canceled, guestErr, annotationsErr, stdinErr, outputErr, hostErr, runtimeErr} {
		if !errors.Is(result, cause) {
			t.Errorf("cleanup lost %v: %v", cause, result)
		}
	}
	for _, label := range []string{"close component", "close source annotations", "close stdin", "close host imports", "close runtime"} {
		if !strings.Contains(result.Error(), label) {
			t.Errorf("cleanup error missing %q: %v", label, result)
		}
	}
	wantOrder := []string{"guest", "guest", "annotations", "stdin", "output", "host", "runtime", "runtime"}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("cleanup order %v; want %v", order, wantOrder)
	}
	if p.engine != nil || teardown.host != nil || teardown.world != nil || teardown.output != nil || p.memory.used != 0 {
		t.Fatalf("cleanup incomplete: runtime=%v host=%v world=%v output=%v memory=%d", p.engine != nil, teardown.host != nil, teardown.world != nil, teardown.output != nil, p.memory.used)
	}
	if teardown.guest != nil || teardown.annotations != nil || teardown.stdin != nil {
		t.Fatal("cleanup retained call-owned instances")
	}
	if _, err := compiled.Instantiate(t.Context()); err == nil {
		t.Fatal("failed guest cleanup left the runtime usable")
	}
	// A later cleanup must tolerate the retired runtime and preserve the error.
	before := result.Error()
	teardown.finish(cancelled, &result)
	if result.Error() != before {
		t.Fatalf("repeated cleanup changed the result: %v", result)
	}
}

func TestCallScopeClosesPartialImportsAfterFailure(t *testing.T) {
	p, err := Load(t.Context(), filepath.Join("testdata", "wat", "close-failure.wasm"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	teardown := callScope{plugin: p}
	teardown.host, err = p.hostFactory.NewInstance(t.Context(), hostImplementation{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("partial host cleanup")
	var order []string
	teardown.host.RegisterResource(teardownDrop{t, "host", closeErr, &order})
	if err := teardown.discardImports(t.Context()); !errors.Is(err, closeErr) {
		t.Fatalf("partial cleanup failure: %v", err)
	}
	if !slices.Equal(order, []string{"host"}) || teardown.host != nil || teardown.world != nil || teardown.output != nil {
		t.Fatalf("partial cleanup incomplete: order=%v host=%v world=%v output=%v", order, teardown.host != nil, teardown.world != nil, teardown.output != nil)
	}
	if _, err := p.callAnalyzer(t.Context(), "describe"); err != nil {
		t.Fatalf("call after failed partial cleanup: %v", err)
	}
	if !slices.Equal(order, []string{"host"}) {
		t.Fatalf("partial resource closed more than once: %v", order)
	}
}

// A budget panic must retain errors collected while unwinding the invocation.
func TestCallScopeMemoryPanicPreservesCleanupFailure(t *testing.T) {
	p, err := Load(t.Context(), filepath.Join("testdata", "wat", "close-failure.wasm"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	data, err := os.ReadFile(filepath.Join("testdata", "wat", "aggregate-tables.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	p.component, err = p.engine.LoadComponent(t.Context(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	scope := callScope{plugin: p}
	cleanupErr := errors.New("host cleanup after memory panic")
	var order []string
	ctx := experimental.WithMemoryAllocator(t.Context(), p.memory)
	ctx = wacogo.WithCoreTableAllocator(ctx, func(tables []wacogo.CoreTable) (func(), error) {
		if scope.world != nil {
			scope.host.RegisterResource(teardownDrop{t, "host", cleanupErr, &order})
			panic(errMemoryBudget)
		}
		return p.memory.allocateTables(tables)
	})
	_, err = scope.invoke(ctx, AnalyzerInterface, "describe")
	if !errors.Is(err, errMemoryBudget) || !errors.Is(err, cleanupErr) {
		t.Fatalf("memory panic lost budget or cleanup error: %v", err)
	}
	if !slices.Equal(order, []string{"host"}) || p.engine != nil || p.memory.used != 0 {
		t.Fatalf("cleanup incomplete: order=%v runtime=%v memory=%d", order, p.engine != nil, p.memory.used)
	}
}

type teardownDrop struct {
	t     *testing.T
	name  string
	err   error
	order *[]string
}

func (d teardownDrop) Drop(ctx context.Context) error {
	d.t.Helper()
	if err := ctx.Err(); err != nil {
		d.t.Errorf("%s cleanup received cancelled context: %v", d.name, err)
	}
	*d.order = append(*d.order, d.name)
	return d.err
}
