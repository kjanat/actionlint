package component

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	hostbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/host"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/wasmparser"
	"github.com/kjanat/wazero/experimental"
	"golang.org/x/sync/semaphore"
)

func TestComponentTableReservationsShareLinearMemoryBudget(t *testing.T) {
	budget := &memoryBudget{limit: 128}
	table := wacogo.CoreTable{Min: 0, Max: wasmparser.Some(uint32(2))}
	release, err := budget.allocateTables([]wacogo.CoreTable{table})
	if err != nil {
		t.Fatal(err)
	}
	memory := budget.Allocate(0, 128)
	if got := memory.Reallocate(64); len(got) != 64 || budget.used != 128 {
		t.Fatalf("table and linear allocations not shared: bytes=%d used=%d", len(got), budget.used)
	}
	if memory.Reallocate(65) != nil {
		t.Fatal("linear growth ignored table reservation")
	}
	if _, err := budget.allocateTables([]wacogo.CoreTable{table}); !errors.Is(err, errMemoryBudget) {
		t.Fatalf("table reservation ignored linear allocation: %v", err)
	}
	release()
	release()
	if got := memory.Reallocate(128); len(got) != 128 || budget.used != 128 {
		t.Fatalf("released table capacity unusable: bytes=%d used=%d", len(got), budget.used)
	}
	memory.Free()
	if budget.used != 0 {
		t.Fatalf("closed allocations still account for %d bytes", budget.used)
	}
	for _, table := range []wacogo.CoreTable{{Min: 0}, {Min: 0, Max: wasmparser.Some(uint32(math.MaxUint32))}} {
		if _, err := budget.allocateTables([]wacogo.CoreTable{table}); err == nil || budget.used != 0 {
			t.Fatalf("unsafe table admitted: table=%+v used=%d err=%v", table, budget.used, err)
		}
	}
}

func TestComponentNestedTablesBoundMemoryGrowthAndRelease(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "wat", "aggregate-tables.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	// Two instances reserve 2 * (2 elements * 32 bytes), alongside their memories.
	budget := &memoryBudget{limit: 3<<16 + 128}
	ctx := wacogo.WithCoreTableAllocator(experimental.WithMemoryAllocator(t.Context(), budget), budget.allocateTables)
	engine := wacogo.NewEngine(ctx)
	t.Cleanup(func() { _ = engine.Close(context.WithoutCancel(ctx)) })
	component, err := engine.LoadComponent(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		instance, err := component.Instantiate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if budget.used != 2<<16+128 {
			t.Fatalf("nested/repeated table not charged: used=%d", budget.used)
		}
		grow := instance.ExportedFunc("grow")
		if grow == nil {
			t.Fatal("fixture has no grow export")
		}
		for _, want := range []uint32{1, math.MaxUint32} {
			results, err := grow.Call(ctx)
			if err != nil || len(results) != 1 || results[0] != wacogo.ValU32(want) {
				t.Fatalf("shared growth budget: results=%v err=%v want=%d", results, err, want)
			}
		}
		if budget.used != budget.limit {
			t.Fatalf("growth lost table reservation: used=%d limit=%d", budget.used, budget.limit)
		}
		if err := instance.Close(ctx); err != nil || budget.used != 0 {
			t.Fatalf("nested teardown leaked table reservation: used=%d err=%v", budget.used, err)
		}
	}
}

func TestPluginCallAccountsForCoreTables(t *testing.T) {
	path := filepath.Join("testdata", "wat", "aggregate-tables.wasm")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	engine := wacogo.NewEngine(t.Context())
	factory, err := hostbind.NewFactory(t.Context(), engine)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := engine.LoadComponent(t.Context(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(filepath.Join("testdata", "wat", "aggregate-memory.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	withoutTables, err := engine.LoadComponent(t.Context(), bytes.NewReader(plain))
	if err != nil {
		t.Fatal(err)
	}
	plugin := &Plugin{path: path, engine: engine, hostFactory: factory, component: withoutTables,
		limits: Limits{Timeout: time.Second, MemoryPages: 64}, gate: semaphore.NewWeighted(1),
		memory: &memoryBudget{limit: 64 << 16}, log: func(string) {}}
	t.Cleanup(func() { _ = plugin.Close(context.Background()) })
	if _, err := plugin.callAnalyzer(t.Context(), "describe"); err != nil {
		t.Fatal(err)
	}
	// Host instances vary independently of the guest. Leave exactly
	// one page for stdin's stub and two pages for the guest's core instances.
	plugin.memory.limit = callImportMemory(t, plugin) + 3<<16
	plugin.limits.MemoryPages = uint32(plugin.memory.limit >> 16)
	if _, err := plugin.callAnalyzer(t.Context(), "describe"); err != nil {
		t.Fatalf("linear-only fixture did not fit calibrated budget: %v", err)
	}
	plugin.component = fixture
	if _, err := plugin.callAnalyzer(t.Context(), "describe"); !errors.Is(err, errMemoryBudget) {
		t.Fatalf("real plugin call ignored table reservations: %v", err)
	}
	if plugin.engine != nil || plugin.memory.used != 0 {
		t.Fatalf("failed combined allocation leaked: runtime=%v used=%d", plugin.engine, plugin.memory.used)
	}
}
