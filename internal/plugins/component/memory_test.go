package component

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/kjanat/wacogo"
	"github.com/tetratelabs/wazero/experimental"
)

func callImportMemory(t *testing.T, plugin *Plugin) uint64 {
	t.Helper()
	baseline := plugin.memory.used
	ctx := experimental.WithMemoryAllocator(t.Context(), plugin.memory)
	ctx = wacogo.WithCoreTableAllocator(ctx, plugin.memory.allocateTables)
	scope := callScope{plugin: plugin}
	if err := scope.prepareImports(ctx); err != nil {
		t.Fatal(err)
	}
	used := plugin.memory.used
	if err := scope.discardImports(context.WithoutCancel(ctx)); err != nil {
		t.Fatal(err)
	}
	if plugin.memory.used != baseline {
		t.Fatalf("host cleanup left %d bytes accounted; want %d", plugin.memory.used, baseline)
	}
	return used
}

func TestComponentAggregateMemoryGrowthAndRelease(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "wat", "aggregate-memory.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	budget := &memoryBudget{limit: 3 << 16}
	ctx := experimental.WithMemoryAllocator(t.Context(), budget)
	engine := wacogo.NewEngine(ctx)
	t.Cleanup(func() { _ = engine.Close(context.Background()) })
	component, err := engine.LoadComponent(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		instance, err := component.Instantiate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		grow := instance.ExportedFunc("grow")
		if grow == nil {
			t.Fatal("fixture has no grow export")
		}
		for _, want := range []uint32{1, math.MaxUint32} {
			results, err := grow.Call(ctx)
			if err != nil || len(results) != 1 || results[0] != wacogo.ValU32(want) {
				t.Fatalf("aggregate growth: results=%v, err=%v, want=%d", results, err, want)
			}
		}
		if budget.used != budget.limit {
			t.Fatalf("want three live pages, got %d bytes", budget.used)
		}
		if err := instance.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if budget.used != 0 {
			t.Fatalf("closed instance retained %d accounted bytes", budget.used)
		}
	}
}

func TestMemoryBudgetPreservesContentsAndReusesReleasedCapacity(t *testing.T) {
	budget := &memoryBudget{limit: 12}
	first, second := budget.Allocate(1024, 12), budget.Allocate(1024, 12)
	copy(first.Reallocate(4), "data")
	second.Reallocate(4)
	if got := first.Reallocate(8); string(got[:4]) != "data" || !bytes.Equal(got[4:], make([]byte, 4)) {
		t.Fatalf("growth changed contents or failed to zero new bytes: %v", got)
	}
	if first.Reallocate(9) != nil || budget.used != 12 {
		t.Fatal("growth exceeded the shared budget")
	}
	second.Free()
	second.Free()
	if got := first.Reallocate(12); len(got) != 12 || string(got[:4]) != "data" {
		t.Fatalf("released capacity was not reusable: %v", got)
	}
	first.Free()
	if budget.used != 0 || first.Reallocate(1) != nil {
		t.Fatal("freed memory remained accounted or usable")
	}
}
