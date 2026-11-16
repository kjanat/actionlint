package assembly

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

func TestRequiredToolsWithPluginsUsesRemoteConfigConnections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config string
		want         bool
	}{
		{"enabled", "{}\n", true},
		{"disabled", "lint: {rules: {external: {shellcheck: off}}}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			remote := "https://config.example/base.yaml"
			fixture := fixtureregistry.NewPluginRegistry(t)
			fixture.SetFile(remote, []byte(tc.config))
			root := t.TempDir()
			config := filepath.Join(root, "actionlint.yaml")
			workflow := filepath.Join(root, "workflow.yml")
			for path, content := range map[string]string{
				config:   "extends: ['" + remote + "']\n",
				workflow: "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo hello}]\n",
			} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			options := checking.Options{WorkingDir: root, ConfigFile: config, Tools: registry.Registrations(registry.ToolSelections{"shellcheck": {Command: "shellcheck"}})}
			sources := pluginselection.Sources{Acquisition: dependencycache.Acquisition{UserCacheDir: fixture.UserCacheDir(), Connections: fixture.Connections()}}
			needed, err := RequiredToolsWithPlugins(t.Context(), options, []string{workflow}, sources)
			if err != nil || needed["shellcheck"] != tc.want {
				t.Fatalf("required tools = %v, error = %v; want shellcheck=%t", needed, err, tc.want)
			}
			if requests := fixture.Requests(); len(requests) != 1 || requests[0] != remote {
				t.Fatalf("requests = %v, want [%s]", requests, remote)
			}
		})
	}
}
