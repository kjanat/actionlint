package localapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestHydratedPluginsLoadAfterCachedArtifactRemoved(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	module, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := pluginregistry.OpenLock(t.Context(), filepath.Join(t.TempDir(), pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry.Requests = nil
	sources, err := (PluginSelection{LockFile: lock.Path}).Resolve(t.Context(), t.TempDir(), application.Input{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(artifactOptions().CacheDir, pin.SHA256)); err != nil {
		t.Fatal(err)
	}
	// Remove the remote bytes: every later consumer must use the verified snapshot.
	delete(registry.Files, pin.URL)
	if _, err := RulesSources(t.Context(), sources); err != nil {
		t.Fatal(err)
	}
	result, err := CheckSources(t.Context(), application.Options{}, application.Input{
		Kind: application.Stdin, Reader: strings.NewReader("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo echo hello}]\n"),
	}, sources)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range result.Diagnostics {
		found = found || finding.Rule == "no-sudo"
	}
	if !found {
		t.Fatalf("verified plugin did not analyze the script: %+v", result.Diagnostics)
	}
	session, err := newEditorSources(t.Context(), application.Options{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	downloads := 0
	for _, request := range registry.Requests {
		if request == pin.URL {
			downloads++
		}
	}
	if downloads != 1 {
		t.Fatalf("artifact downloaded %d times, want 1", downloads)
	}
}
