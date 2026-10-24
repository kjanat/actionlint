package component

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	hostbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/host"
	"github.com/kjanat/wacogo"
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
	plugin := &Plugin{path: path, engine: engine, hostFactory: factory, component: fixture,
		limits: Limits{Timeout: time.Second, MemoryPages: 1}, gate: semaphore.NewWeighted(1),
		memory: &memoryBudget{limit: 1 << 16}, log: func(string) {}}
	t.Cleanup(func() { _ = plugin.Close(context.Background()) })
	if _, err = plugin.callAnalyzer(t.Context(), "describe"); err == nil {
		t.Fatal("instantiated two one-page memories under a one-page plugin limit")
	}
	if !errors.Is(err, errMemoryBudget) {
		t.Fatalf("want aggregate memory failure, got %v", err)
	}
	if plugin.engine != nil || plugin.memory.used != 0 {
		t.Fatal("failed initial allocation retained a live runtime or accounted memory")
	}
}
