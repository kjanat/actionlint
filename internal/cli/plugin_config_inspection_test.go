package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestConfigInspectionLoadsDeclaredRules(t *testing.T) {
	wasm, err := os.ReadFile("internal/plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", wasm)
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", testutil.FixtureRegistryURL)
	path := filepath.Join(".github", "actionlint.yaml")
	manifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	for _, setting := range []string{
		"lint: {rules: {plugins: {no-sudo: off}}}",
		"lint: {rules: {disable: [no-sudo]}}",
		"ignores: [{rule: no-sudo, file: '**'}]",
	} {
		t.Run(setting, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(string(manifest)+"\n"+setting+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			pluginCommandTest(t, 0, "config", "validate", "--json")
			pluginCommandTest(t, 0, "config", "show", "--origin", "--json")
			pluginCommandTest(t, 0, "config", "validate", "--config", path)
		})
	}
	if err := os.WriteFile(path, []byte(string(manifest)+"\nlint: {rules: {disable: [unregistered-plugin]}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output := pluginCommandTest(t, 3, "config", "validate"); !strings.Contains(output, "unknown diagnostic rule") {
		t.Fatal(output)
	}
	if len(registry.Requests()) != requests {
		t.Fatal("config inspection contacted the offline registry")
	}
}
