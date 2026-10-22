package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/pluginregistry"
	"actionlint.kjanat.dev/internal/testutil"
)

func TestCheckPluginRuntimeFailureExitStatus(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("workflow.yml", []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installed := pluginCommandTest(t, 0, "plugin", "install", testutil.FixturePluginName, "--registry", testutil.FixtureRegistryURL, "--json")
	var lock pluginregistry.Lock
	if err := json.Unmarshal([]byte(installed), &lock); err != nil || len(lock.Plugins) != 1 {
		t.Fatalf("invalid installed lock: %s: %v", installed, err)
	}
	for _, scenario := range []struct {
		name   string
		args   []string
		status int
	}{
		{"automatic", nil, 3},
		{"lock", []string{"--plugin-lock", filepath.Join(root, ".github", pluginregistry.LockName)}, 3},
		{"config", []string{"--config", filepath.Join(root, ".github", "actionlint.yaml")}, 3},
		{"explicit", []string{"--plugin", lock.Plugins[0].Source()}, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			args := append([]string{"check", "--no-ruff", "--no-shellcheck", "workflow.yml"}, scenario.args...)
			output := pluginCommandTest(t, scenario.status, args...)
			if !strings.Contains(output, "export") {
				t.Fatalf("did not reach runtime export validation: %s", output)
			}
		})
	}
}
