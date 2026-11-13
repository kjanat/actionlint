package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestEditorSessionRejectsRevocationRecordedAfterStartup(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	wasm, err := os.ReadFile("../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", wasm)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	lock := plugininstall.Lock{SchemaVersion: 1, Plugins: []plugininstall.Pin{pin}}
	sources := plugininstall.Sources{Paths: []string{pin.Source()}, Releases: &lock, Acquisition: acquisition}
	root := t.TempDir()
	options := checking.Options{WorkingDir: root, SkipProjectConfig: true}
	session, err := newEditorSources(t.Context(), options, sources)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(root, "workflow.yml")
	documents := map[string]Document{
		path: {
			URI: FileURI(path), Version: 1,
			Text: "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo first\npermissions: {}\n",
		},
	}
	result, err := session.analyze(t.Context(), documents)
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "no-sudo" {
		t.Fatalf("initial plugin analysis: %+v %v", result, err)
	}
	status := plugininstall.ReleaseStatus{
		Name: pin.Name, Version: pin.Version, SHA256: pin.SHA256, Status: plugininstall.ReleaseRevoked,
		Reason: "Publisher revoked this artifact", ChangedAt: time.Now().UTC(),
		Actor: plugininstall.StatusActor{
			Kind: "registry-admin", ID: "123", Name: "maintainer",
			Workflow: "owner/registry/.github/workflows/status.yml@refs/heads/master", RunID: "10:1",
		},
	}
	data, err := json.Marshal(plugininstall.StatusSnapshot{SchemaVersion: 1, Releases: []plugininstall.ReleaseStatus{status}})
	if err != nil {
		t.Fatal(err)
	}
	registry.SetFile(fixtureregistry.FixtureRegistryURL+"statuses.json", data)
	if _, err := client.RefreshStatuses(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	result, err = session.analyze(t.Context(), documents)
	if _, ok := errors.AsType[*pluginexec.LoadError](err); !ok || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revocation became clean analysis: %+v %v", result, err)
	}
	if len(result.Diagnostics) != 0 || len(registry.Requests()) != requests {
		t.Fatalf("revoked plugin ran or accessed network: %+v", result)
	}
	// Startup must also refuse cached revocation before loading any guest.
	if next, err := newEditorSources(t.Context(), options, sources); err == nil {
		if closeErr := next.close(t.Context()); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("new session accepted revoked plugin")
	}
	if len(registry.Requests()) != requests {
		t.Fatal("revocation guard accessed the registry")
	}
}
