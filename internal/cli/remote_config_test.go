package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestRemoteConfigUsesCommandConnections(t *testing.T) {
	t.Parallel()
	component, err := filepath.Abs("internal/plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"validate", "show", "check"} {
		for _, explicit := range []bool{false, true} {
			name := operation + "/configured"
			if explicit {
				name = operation + "/explicit"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				registry := fixtureregistry.NewPluginRegistry(t)
				acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
				const remote = "https://config.example/base.yaml"
				registry.SetFile(remote, []byte("output: {formats: [json], summary: false}\nlint: {rules: {plugins: {no-sudo: off}}}\n"))
				config, workflow := configuredOutputFixture(t, "extends: ['"+remote+"']\n")
				wasm, err := os.ReadFile(component)
				if err != nil {
					t.Fatal(err)
				}
				const artifact = "https://config.example/no-sudo.wasm"
				registry.SetFile(artifact, wasm)
				source := fmt.Sprintf("%s#sha256=%x", artifact, sha256.Sum256(wasm))
				pluginPath, err := json.Marshal(source)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, []byte("plugins: ["+string(pluginPath)+"]\nextends: ['"+remote+"']\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				args := []string{"config", operation, "--json", "--config", config}
				status := 0
				if operation == "check" {
					args = []string{"check", "--config", config, "--no-tool=shellcheck", "--no-tool=ruff", "--online", workflow}
					status = 1
				}
				if explicit {
					args = append(args, "--plugin", component)
				}
				output := pluginCommandTest(t, acquisition, status, args...)
				if operation == "check" && (!json.Valid([]byte(output)) || !strings.Contains(output, "empty-run")) {
					t.Fatalf("inherited JSON output missing diagnostic: %s", output)
				}
				if operation == "show" && !strings.Contains(output, "json") {
					t.Fatalf("inherited output missing: %s", output)
				}
				requests := registry.Requests()
				count := 0
				for _, request := range requests {
					if request == remote {
						count++
					}
				}
				if count != 1 {
					t.Fatalf("config requests = %v, want one request for %s", requests, remote)
				}
			})
		}
	}
}
