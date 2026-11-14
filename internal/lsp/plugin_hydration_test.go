package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestHydratedPluginsLoadAfterCachedArtifactRemoved(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	module, err := os.ReadFile("../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "actionlint.yaml")
	content := "plugins: [{name: '" + pin.Name + "', version: '" + pin.Version + "', registry: '" + pin.Registry + "'}]\n"
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	registry.ResetRequests()
	sources, err := (plugininstall.Selection{ConfigFile: config, RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}).Resolve(t.Context(), t.TempDir(), inputmodel.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(acquisition.ArtifactOptions().CacheDir, pin.SHA256)); err != nil {
		t.Fatal(err)
	}
	// Remove the remote bytes: every later consumer must use the verified snapshot.
	registry.DeleteFile(pin.URL)
	if _, err := checking.RulesWithPlugins(t.Context(), sources); err != nil {
		t.Fatal(err)
	}
	result, err := checking.CheckWithPlugins(t.Context(), checking.Options{}, inputmodel.Request{
		Kind: inputmodel.Stdin, Reader: strings.NewReader("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo echo hello}]\npermissions: {}\n"),
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
	session, err := newEditorSources(t.Context(), checking.Options{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	downloads := 0
	for _, request := range registry.Requests() {
		if request == pin.URL {
			downloads++
		}
	}
	if downloads != 1 {
		t.Fatalf("artifact downloaded %d times, want 1", downloads)
	}
}
