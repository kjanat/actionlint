package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestPluginInspectRegistry(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"no-sudo", "no-sudo@v1.0.0", "publisher/plugin@v1.0.0", "github.com/publisher/plugin@v1.0.0"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.SetAliases("no-sudo")
			registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
			var stdout, stderr bytes.Buffer
			command := Command{Stdout: &stdout, Stderr: &stderr, Acquisition: plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}}
			code := command.Main([]string{"actionlint", "plugin", "inspect", source, "--registry", fixtureregistry.FixtureRegistryURL, "--json"})
			if code != 0 {
				t.Fatalf("inspect %s exited %d: %s", source, code, stderr.String())
			}
			var info artifacts.Info
			if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
				t.Fatal(err)
			}
			if info.Kind != artifacts.Component || !bytes.Contains(info.Metadata, []byte(`"plugin_version":"1.0.0"`)) {
				t.Fatalf("inspect %s returned %s with metadata %s", source, info.Kind, info.Metadata)
			}
		})
	}
}
