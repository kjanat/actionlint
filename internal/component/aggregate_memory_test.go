package component

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostbind "actionlint.kjanat.dev/internal/component/bindings/kjanat/actionlint/host"
	"github.com/partite-ai/wacogo"
	"golang.org/x/sync/semaphore"
)

func TestPluginRejectsAggregateInitialMemory(t *testing.T) {
	path := filepath.Join("testdata", "wat", "aggregate-memory.wasm")
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
	plugin := &Plugin{path: path, engine: engine, factory: factory, component: fixture,
		limits: Limits{Timeout: time.Second, MemoryPages: 1}, gate: semaphore.NewWeighted(1),
		memory: &memoryBudget{limit: 1 << 16}, log: func(string) {}}
	t.Cleanup(func() { _ = plugin.Close(context.Background()) })
	if _, err = plugin.call(t.Context(), "describe"); err == nil {
		t.Fatal("instantiated two one-page memories under a one-page plugin limit")
	}
	if !strings.Contains(err.Error(), "aggregate linear memory limit") {
		t.Fatalf("want aggregate memory failure, got %v", err)
	}
	if plugin.engine != nil || plugin.memory.used != 0 {
		t.Fatal("failed initial allocation retained a live runtime or accounted memory")
	}
}
