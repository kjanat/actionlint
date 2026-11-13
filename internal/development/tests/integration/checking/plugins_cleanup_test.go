package checking_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestLoadedPluginsCloseAfterCommandCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	loaded, err := pluginexec.Load(ctx, plugininstall.Sources{Paths: []string{filepath.Join("..", "..", "..", "..", "plugins", "execution", "component", "testdata", "plugins", "pipe-to-shell.wasm")}}, checking.BuiltinRuleNames(), io.Discard, false)
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
	if _, err := plugin.CheckDocument(t.Context(), checkinputs.Input{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("component remains usable after canceled command cleanup: %v", err)
	}
	if len(loaded.Components) != 0 {
		t.Fatal("closed components remain")
	}
}
