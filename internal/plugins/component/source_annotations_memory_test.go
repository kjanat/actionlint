package component

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
)

func TestSourceAnnotationsShareTheCallMemoryBudget(t *testing.T) {
	plugin, err := Load(t.Context(), filepath.Join("testdata", "plugins", "source-annotations.wasm"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plugin.Close(context.WithoutCancel(t.Context())) })
	if plugin.annotationsFactory == nil {
		t.Fatal("annotated component did not create its optional import factory")
	}
	// Leave one page beyond the host imports for stdin and none for annotations.
	plugin.memory.limit = callImportMemory(t, plugin) + 1<<16
	plugin.limits.MemoryPages = uint32(plugin.memory.limit >> 16)
	if _, err := plugin.Analyze(t.Context(), analyzer.Input{}); !errors.Is(err, errMemoryBudget) {
		t.Fatalf("annotations ignored the shared memory budget: %v", err)
	}
	if plugin.engine != nil || plugin.memory.used != 0 {
		t.Fatalf("failed annotation allocation leaked resources: runtime=%v used=%d", plugin.engine, plugin.memory.used)
	}
}
