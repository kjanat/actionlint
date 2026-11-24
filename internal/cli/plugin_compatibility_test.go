package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func TestPluginCommandsSelectLoadableReleases(t *testing.T) {
	r := fixtureregistry.NewPluginRegistry(t)
	acquisition := dependencycache.Acquisition{UserCacheDir: r.UserCacheDir(), Connections: r.Connections()}
	r.Add(t, "v1.0.0", fixtureregistry.AnalyzerComponent())
	r.Add(t, "v2.0.0", wasmcomponent.Build(nil, []string{"kjanat:actionlint/analyzer@99.0.0"}, nil))
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Setenv("ACTIONLINT_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	reason := "unprovided interface: plugin exports kjanat:actionlint/analyzer@99.0.0; this host supports " + component.AnalyzerInterface
	listing := pluginCommandTest(t, acquisition, 0, "plugin", "versions", "publisher/plugin", "--registry", fixtureregistry.FixtureRegistryURL)
	if listing != "v1.0.0\tcompatible\nv2.0.0\tincompatible: "+reason+"\n" {
		t.Fatalf("versions listing:\n%s", listing)
	}
	var listed []struct {
		Version string                     `json:"version"`
		Exports []string                   `json:"exports"`
		Host    pluginregistry.HostSupport `json:"host"`
	}
	if err := json.Unmarshal([]byte(pluginCommandTest(t, acquisition, 0, "plugin", "versions", "publisher/plugin", "--registry", fixtureregistry.FixtureRegistryURL, "--json")), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Host != (pluginregistry.HostSupport{Status: pluginregistry.SupportCompatible}) || listed[1].Host != (pluginregistry.HostSupport{Status: pluginregistry.SupportIncompatible, Reason: reason}) || len(listed[1].Exports) != 1 {
		t.Fatalf("JSON listing: %+v", listed)
	}
	configPath := filepath.Join(".github", "actionlint.yaml")
	refused := pluginCommandTest(t, acquisition, 3, "plugin", "install", "publisher/plugin@v2.0.0", "--registry", fixtureregistry.FixtureRegistryURL)
	if !strings.Contains(refused, fixtureregistry.FixturePluginName+"@v2.0.0 does not load in this actionlint: "+reason) {
		t.Fatalf("exact incompatible install: %s", refused)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("refused install wrote configuration: %v", err)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin", "--registry", fixtureregistry.FixtureRegistryURL)
	config, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(config), "version: v1.0.0") {
		t.Fatalf("latest install wrote %s: %v", config, err)
	}
}
