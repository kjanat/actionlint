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

	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/wasi/cli/stdin"
	"github.com/tetratelabs/wazero/experimental"
)

func TestCallTeardownPreservesFailuresAndClosesRuntimeLast(t *testing.T) {
	p, err := Load(t.Context(), filepath.Join("testdata", "plugins", "source-annotations.wasm"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	ctx := experimental.WithMemoryAllocator(t.Context(), p.memory)
	if err := p.prepareImports(ctx); err != nil {
		t.Fatal(err)
	}
	input, err := stdin.NewInstance(ctx, p.engine, p.world.Streams, p.world.Error, p.world.Poll, nil)
	if err != nil {
		t.Fatal(err)
	}
	annotations, err := p.annotationsFactory.NewInstance(ctx, sourceAnnotations{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	stdinErr := errors.New("stdin cleanup")
	annotationsErr := errors.New("annotations cleanup")
	hostErr := errors.New("host cleanup")
	input.RegisterResource(teardownDrop{t, "stdin", stdinErr, &order})
	annotations.RegisterResource(teardownDrop{t, "annotations", annotationsErr, &order})
	p.host.RegisterResource(teardownDrop{t, "host", hostErr, &order})

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
	teardown := callTeardown{plugin: p}
	func() {
		defer teardown.finish(cancelled, &result)
		defer teardown.closeResource(cancelled, "stdin", input, &result)
		defer teardown.closeResource(cancelled, "source annotations", annotations, &result)
		defer teardown.closeGuest(cancelled, guest, &result)
	}()
	for _, cause := range []error{primary, context.Canceled, guestErr, annotationsErr, stdinErr, hostErr, runtimeErr} {
		if !errors.Is(result, cause) {
			t.Errorf("cleanup lost %v: %v", cause, result)
		}
	}
	for _, label := range []string{"close component", "close source annotations", "close stdin", "close host imports"} {
		if !strings.Contains(result.Error(), label) {
			t.Errorf("cleanup error missing %q: %v", label, result)
		}
	}
	wantOrder := []string{"guest", "guest", "annotations", "stdin", "host", "runtime", "runtime"}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("cleanup order %v; want %v", order, wantOrder)
	}
	if p.engine != nil || p.host != nil || p.world != nil || p.output != nil || p.memory.used != 0 {
		t.Fatalf("cleanup incomplete: runtime=%v host=%v world=%v output=%v memory=%d", p.engine != nil, p.host != nil, p.world != nil, p.output != nil, p.memory.used)
	}
	if _, err := compiled.Instantiate(t.Context()); err == nil {
		t.Fatal("failed guest cleanup left the runtime usable")
	}
	// A later cleanup must tolerate the retired runtime and preserve the error.
	before := result
	teardown.finish(cancelled, &result)
	if result != before {
		t.Fatalf("repeated cleanup changed the result: %v", result)
	}
}

func TestPrepareImportsStopsAfterPartialCleanupFailure(t *testing.T) {
	p, err := Load(t.Context(), filepath.Join("testdata", "wat", "close-failure.wasm"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	p.host, err = p.factory.NewInstance(t.Context(), hostImplementation{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("partial host cleanup")
	var order []string
	p.host.RegisterResource(teardownDrop{t, "host", closeErr, &order})
	if err := p.prepareImports(t.Context()); !errors.Is(err, closeErr) {
		t.Fatalf("partial cleanup failure: %v", err)
	}
	if !slices.Equal(order, []string{"host"}) || p.host != nil || p.world != nil || p.output != nil {
		t.Fatalf("partial cleanup incomplete: order=%v host=%v world=%v output=%v", order, p.host != nil, p.world != nil, p.output != nil)
	}
	if _, err := p.call(t.Context(), "describe"); err != nil {
		t.Fatalf("call after failed partial cleanup: %v", err)
	}
	if !slices.Equal(order, []string{"host"}) {
		t.Fatalf("partial resource closed more than once: %v", order)
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
