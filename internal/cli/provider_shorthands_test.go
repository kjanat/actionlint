package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixtureenvironment "actionlint.kjanat.dev/v2/internal/development/tests/support/environment"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
)

func TestInstalledProviderShorthands(t *testing.T) {
	providers, err := manifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	var provider manifests.Manifest
	for _, p := range providers {
		if p.Rule.Name == "shellcheck" {
			provider = p
		}
	}
	provider.ID = "example.com/file-tool"
	provider.Rule.Name = "file-tool"
	provider.ConfigSchema = json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1},"nested":{"type":"object","properties":{"mode":{"enum":["quiet","loud"]}},"additionalProperties":false}},"additionalProperties":false}`)
	provider.ConfigShorthands = map[string]configschema.Shorthand{
		"limit": {Path: []string{"limit"}, Precedence: configschema.PrecedenceShorthand},
		"mode":  {Path: []string{"nested", "mode"}, Precedence: configschema.PrecedenceConfig},
	}
	provider.ConfigurationTransform = nil
	provider.ConfigurationPaths = nil
	provider.PreparationTransform = nil
	provider.Integration = nil
	provider.DeprecatedFlags = nil
	provider.Process.Args = []string{}
	provider.Process.ConfigSchema = nil
	provider.Process.ConfigFiles = nil
	provider.Process.Output.Format = "sarif@2.1.0"
	provider.Process.Output.Transform = nil
	data, err := json.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "provider.manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	config := filepath.Join(root, "actionlint.toml")
	base := []byte("[tools.file-tool]\nlimit = 9\n")
	if err := os.WriteFile(filepath.Join(root, "base.toml"), base, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := data
	for _, tc := range []struct {
		name, source, want string
		code               int
	}{
		{"extends shorthand", "extends = ['base.toml']\n[tools.file-tool]\nconfig = { limit = 5 }\n", `"limit":5`, 0},
		{"extends config", "extends = ['base.toml']\n[tools.file-tool]\nlimit = 5\n", `"limit":5`, 0},
		{"root", "[tools.file-tool]\nlimit = 5\n", `"limit":5`, 0},
		{"shorthand wins", "[tools.file-tool]\nlimit = 5\nconfig = { limit = 3 }\n", `"limit":5`, 0},
		{"config wins", "[tools.file-tool]\nmode = 'loud'\nconfig = { nested = { mode = 'quiet' } }\n", `"mode":"quiet"`, 0},
		{"override", "[[overrides]]\nincludes = ['**']\n[overrides.tools.file-tool]\nlimit = 7\n", `"limit":7`, 0},
		{"invalid root", "[tools.file-tool]\nlimit = 0\n", "actionlint.toml:2:", 3},
		{"invalid override", "[[overrides]]\nincludes = ['**']\n[overrides.tools.file-tool]\nlimit = 0\n", "actionlint.toml:4:", 3},
		{"unknown key", "[tools.file-tool]\nunknown = 1\n", "unknown key", 3},
		{"path and shorthand", "[tools.file-tool]\nlimit = 5\nconfig = 'tool.toml'\n", "cannot be combined", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			path := filepath.Join(directory, "provider.manifest.json")
			config := filepath.Join(directory, "actionlint.toml")
			for file, content := range map[string][]byte{path: manifest, filepath.Join(directory, "base.toml"): base, config: []byte(tc.source)} {
				if err := os.WriteFile(file, content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"config", "show", "--json", "--plugin", path, "--config", config}
			output := pluginCommandTest(t, tc.code, args...)
			if !strings.Contains(output, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, output)
			}
			pluginCommandTest(t, tc.code, "config", "validate", "--plugin", path, "--config", config)
		})
	}
	if err := os.WriteFile(config, []byte("[tools.file-tool]\nlimit = 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := pluginCommandTest(t, 0, "config", "schema", "--plugin", path, "--config", config)
	schema, err := configschema.Compile([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source string
		valid  bool
	}{
		{`{"tools":{"file-tool":{"limit":5,"mode":"quiet"}}}`, true},
		{`{"overrides":[{"includes":["**"],"tools":{"file-tool":{"limit":5}}}]}`, true},
		{`{"tools":{"file-tool":{"limit":0}}}`, false},
		{`{"overrides":[{"includes":["**"],"tools":{"file-tool":{"mode":"invalid"}}}]}`, false},
		{`{"tools":{"file-tool":{"limit":5,"config":"tool.toml"}}}`, false},
	} {
		if err := schema.Validate([]byte(tc.source)); (err == nil) != tc.valid {
			t.Errorf("%s: %v", tc.source, err)
		}
	}
	workflow := filepath.Join(root, "workflow.yml")
	if err := os.WriteFile(workflow, []byte("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("extends = ['base.toml']\n[tools]\nshellcheck = false\nruff = false\n[tools.file-tool]\nenabled = false\n[[overrides]]\nincludes = ['**']\n[overrides.tools.file-tool]\nlimit = 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "check", "--plugin", path, "--config", config, workflow)
	if _, err := checking.RequiredToolsWithPluginPaths(t.Context(), checking.Options{ConfigFile: config}, []string{workflow}, []string{path}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	fixtureenvironment.IsolateUserCache(t)
	network := fixtureregistry.NewPluginRegistry(t)
	const source = "https://example.com/provider.manifest.json"
	network.SetFile(source, data)
	declared := fmt.Sprintf("plugins = ['%s#sha256=%x']\n%s", source, sha256.Sum256(data), content)
	if err := os.WriteFile(config, []byte(declared), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "config", "validate", "--config", config)
	pluginCommandTest(t, 0, "check", "--allow-config-processes", "--config", config, workflow)
	if err := os.WriteFile(config, []byte(strings.ReplaceAll(declared, "limit = 7", "unknown = 7")), 0o600); err != nil {
		t.Fatal(err)
	}
	if output := pluginCommandTest(t, 3, "check", "--allow-config-processes", "--config", config, workflow); !strings.Contains(output, "unknown key") {
		t.Fatalf("unknown shorthand was not rejected after plugin loading: %s", output)
	}
	provider.Rule.Name = "shellcheck"
	data, err = json.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("[tools]\nruff = false\n[tools.shellcheck]\nenabled = false\nlimit = 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "config", "validate", "--plugin", path, "--config", config)
	pluginCommandTest(t, 0, "check", "--plugin", path, "--config", config, workflow)
}
