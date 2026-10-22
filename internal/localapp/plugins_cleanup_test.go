package localapp

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
)

func TestLoadedPluginsCloseAfterCommandCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	loaded, err := loadPlugins(ctx, []string{filepath.Join("..", "plugins", "component", "testdata", "plugins", "pipe-to-shell.wasm")}, io.Discard, false)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	plugin := loaded.plugins[0]
	t.Cleanup(func() { _ = plugin.Close(t.Context()) })
	cancel()
	if err := loaded.close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Analyze(t.Context(), analyzer.Input{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("component remains usable after canceled command cleanup: %v", err)
	}
	if len(loaded.plugins) != 0 {
		t.Fatal("closed components retained")
	}
}
