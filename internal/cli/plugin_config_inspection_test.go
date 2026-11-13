package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestConfigInspectionAcceptsManagedAndScriptToolReplacements(t *testing.T) {
	component, err := filepath.Abs("internal/plugins/execution/component/testdata/plugins/managed-no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := filepath.Abs("internal/plugins/execution/component/testdata/plugins/ruff-script-engine.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ extension, content string }{
		{"yaml", "plugins: []\nlint: {rules: {correctness: {shellcheck: on, ruff: on}}}\n"},
		{"yml", "plugins: []\nlint: {rules: {correctness: {shellcheck: on, ruff: on}}}\n"},
		{"json", `{"plugins":[],"lint":{"rules":{"correctness":{"shellcheck":"on","ruff":"on"}}}}`},
		{"jsonc", "// Tool replacements\n" + `{"plugins":[],"lint":{"rules":{"correctness":{"shellcheck":"on","ruff":"on"}}},}`},
		{"toml", "plugins = []\n[lint.rules.correctness]\nshellcheck = 'on'\nruff = 'on'\n"},
	} {
		t.Run(tc.extension, func(t *testing.T) {
			t.Chdir(t.TempDir())
			path := "actionlint." + tc.extension
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"validate", "show"} {
				pluginCommandTest(t, plugininstall.Acquisition{}, 0, "config", command, "--json", "--config", path, "--plugin", component, "--plugin", engine)
			}
		})
	}
}

func TestConfigInspectionLoadsDeclaredRules(t *testing.T) {
	wasm, err := os.ReadFile("internal/plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	registry.Add(t, "v1.0.0", wasm)
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", fixtureregistry.FixtureRegistryURL)
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
			project := t.TempDir()
			for _, directory := range []string{".git", ".github"} {
				if err := os.Mkdir(filepath.Join(project, directory), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(project, path), []byte(string(manifest)+"\n"+setting+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Chdir(project)
			pluginCommandTest(t, acquisition, 0, "config", "validate", "--json")
			pluginCommandTest(t, acquisition, 0, "config", "show", "--origin", "--json")
			pluginCommandTest(t, acquisition, 0, "config", "validate", "--config", path)
		})
	}
	if err := os.WriteFile(path, []byte(string(manifest)+"\nlint: {rules: {disable: [unregistered-plugin]}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output := pluginCommandTest(t, acquisition, 3, "config", "validate"); !strings.Contains(output, "unknown diagnostic rule") {
		t.Fatal(output)
	}
	if len(registry.Requests()) != requests {
		t.Fatal("config inspection contacted the offline registry")
	}
}
