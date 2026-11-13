package checking_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
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
			config := writeFixture(t, root, "actionlint.yaml", "extends: ['"+remote+"']\n")
			workflow := writeFixture(t, root, "workflow.yml", "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo hello}]\n")
			options := checking.Options{WorkingDir: root, ConfigFile: config, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
			sources := plugininstall.Sources{Acquisition: plugininstall.Acquisition{UserCacheDir: fixture.UserCacheDir(), Connections: fixture.Connections()}}
			needed, err := checking.RequiredToolsWithPlugins(t.Context(), options, []string{workflow}, sources)
			if err != nil || needed["shellcheck"] != tc.want {
				t.Fatalf("required tools = %v, error = %v; want shellcheck=%t", needed, err, tc.want)
			}
			if requests := fixture.Requests(); len(requests) != 1 || requests[0] != remote {
				t.Fatalf("requests = %v, want [%s]", requests, remote)
			}
		})
	}
}
