package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestRulesPluginFailureExitStatus(t *testing.T) {
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	installed := pluginCommandTest(t, acquisition, 0, "plugin", "install", fixtureregistry.FixturePluginName, "--registry", fixtureregistry.FixtureRegistryURL, "--json")
	var lock plugininstall.ResolvedPlugins
	if err := json.Unmarshal([]byte(installed), &lock); err != nil || len(lock.Plugins) != 1 {
		t.Fatalf("invalid installed lock: %s: %v", installed, err)
	}
	for _, scenario := range []struct {
		name, message string
		args          []string
		status        int
	}{
		{"automatic", "export", nil, 3},
		{"config", "export", []string{"--config", filepath.Join(root, ".github", "actionlint.yaml")}, 3},
		{"explicit invalid", "export", []string{"--plugin", lock.Plugins[0].Source()}, 2},
		{"explicit missing", "missing.wasm", []string{"--plugin", filepath.Join(root, "missing.wasm")}, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			output := pluginCommandTest(t, acquisition, scenario.status, append([]string{"rules"}, scenario.args...)...)
			if !strings.Contains(output, scenario.message) {
				t.Fatalf("unexpected failure: %s", output)
			}
		})
	}
}
