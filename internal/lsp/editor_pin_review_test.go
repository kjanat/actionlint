package lsp

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestEditorEngineRequiresRemotePinBeforeDownload(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	data, err := os.ReadFile("../plugins/execution/module/testdata/engines/echo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	const source = fixtureregistry.FixtureRegistryURL + "engine.wasm"
	registry.SetFile(source, data)
	session, err := newEditorSession(t.Context(), checking.Options{}, nil, plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	for _, suffix := range []string{"", "#sha256=bad"} {
		if _, err := session.options.LoadScriptEngine(t.Context(), source+suffix); err == nil || !strings.Contains(err.Error(), "sha256") {
			t.Fatalf("unpinned source accepted: %v", err)
		}
	}
	if len(registry.Requests()) != 0 {
		t.Fatal("invalid pin reached the network")
	}
	pinned := fmt.Sprintf("%s#sha256=%x", source, sha256.Sum256(data))
	engine, err := session.options.LoadScriptEngine(t.Context(), pinned)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := engine.Check(t.Context(), checkinputs.ScriptRequest{Source: "print(missing)", Filename: "step.py"})
	if err != nil || len(findings) != 1 || findings[0].Code != "F821" {
		t.Fatalf("pinned engine: %+v, %v", findings, err)
	}
	if _, err := session.options.LoadScriptEngine(t.Context(), pinned); err != nil {
		t.Fatal(err)
	}
	if requests := registry.Requests(); len(requests) != 1 || requests[0] != source {
		t.Fatalf("pinned snapshot requests: %v", requests)
	}
}
