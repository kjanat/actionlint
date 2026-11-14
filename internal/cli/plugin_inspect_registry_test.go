package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestPluginInspectMissingLocalFileDoesNotQueryRegistry(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"missing.wasm", "./publisher/plugin.wasm", filepath.Join(t.TempDir(), "plugin.wasm")} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
			status, out, log := runOfflineCommand(t, acquisition, "", "plugin", "inspect", source, "--registry", fixtureregistry.FixtureRegistryURL)
			if status != 3 || !strings.Contains(log, "plugin.wasm") && !strings.Contains(log, "missing.wasm") {
				t.Fatalf("missing local file: status=%d stdout=%s stderr=%s", status, out, log)
			}
			if requests := registry.Requests(); len(requests) != 0 {
				t.Fatalf("local file caused registry requests: %v", requests)
			}
		})
	}
}

func TestPluginInspectRegistry(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"no-sudo", "no-sudo@v1.0.0", "publisher/plugin@v1.0.0", "github.com/publisher/plugin@v1.0.0", "publisher/plugin@release.gz", "publisher/plugin.wasm"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.SetAliases("no-sudo")
			if source == "publisher/plugin.wasm" {
				registry.SetRepository("github.com/publisher/plugin.wasm", 10)
			}
			version := "v1.0.0"
			if _, tag, found := strings.Cut(source, "@"); found {
				version = tag
			}
			registry.Add(t, version, []byte{0, 97, 115, 109, 13, 0, 1, 0})
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
			if info.Kind != artifacts.Component || !bytes.Contains(info.Metadata, []byte(`"plugin_version":"`+strings.TrimPrefix(version, "v")+`"`)) {
				t.Fatalf("inspect %s returned %s with metadata %s", source, info.Kind, info.Metadata)
			}
		})
	}
}
