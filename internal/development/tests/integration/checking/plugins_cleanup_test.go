package checking_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/plugins/execution"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

func TestLoadedPluginsCloseAfterCommandCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	loaded, err := execution.LoadPlugins(ctx, pluginselection.Sources{Paths: []string{filepath.Join("..", "..", "..", "..", "plugins", "execution", "component", "testdata", "plugins", "pipe-to-shell.wasm")}}, checking.BuiltinRuleNames(), io.Discard, false)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	plugin := loaded.Components[0]
	t.Cleanup(func() { _ = plugin.Close(t.Context()) })
	cancel()
	if err := loaded.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.CheckDocument(t.Context(), checkinputs.AnalyzerInput{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("component remains usable after canceled command cleanup: %v", err)
	}
	if len(loaded.Components) != 0 {
		t.Fatal("closed components remain")
	}
}
