package localapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/langserver"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestEditorSessionRejectsRevocationRecordedAfterStartup(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	wasm, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", wasm)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	lock := pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{pin}}
	sources := PluginSources{Paths: []string{pin.Source()}, lock: &lock}
	root := t.TempDir()
	options := application.Options{WorkingDir: root, SkipProjectConfig: true}
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
	documents := map[string]langserver.Document{
		path: {
			URI: langserver.FileURI(path), Version: 1,
			Text: "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo first\n",
		},
	}
	result, err := session.analyze(t.Context(), documents)
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "no-sudo" {
		t.Fatalf("initial plugin analysis: %+v %v", result, err)
	}
	status := pluginregistry.ReleaseStatus{
		Name: pin.Name, Version: pin.Version, SHA256: pin.SHA256, Status: pluginregistry.ReleaseRevoked,
		Reason: "Publisher revoked this artifact", ChangedAt: time.Now().UTC(),
		Actor: pluginregistry.StatusActor{
			Kind: "registry-admin", ID: "123", Name: "maintainer",
			Workflow: "owner/registry/.github/workflows/status.yml@refs/heads/master", RunID: "10:1",
		},
	}
	data, err := json.Marshal(pluginregistry.StatusSnapshot{SchemaVersion: 1, Releases: []pluginregistry.ReleaseStatus{status}})
	if err != nil {
		t.Fatal(err)
	}
	registry.SetFile(testutil.FixtureRegistryURL+"statuses.json", data)
	if _, err := client.RefreshStatuses(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	result, err = session.analyze(t.Context(), documents)
	if _, ok := errors.AsType[*PluginLoadError](err); !ok || !strings.Contains(err.Error(), "revoked") {
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
