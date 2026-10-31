package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfigMigrationCommand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Setenv("ACTIONLINT_CONFIG", "")
	path := filepath.Join(root, "config.yaml")
	original := []byte("plugins: []\nconfig-variables: []\npolicy: {cache-operation: false}\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	preview := pluginCommandTest(t, 0, "config", "migrate", "--config", path)
	if !strings.Contains(preview, "expression-reference") || !strings.Contains(preview, "presets and --strict") {
		t.Fatal(preview)
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(current, original) {
		t.Fatal("preview wrote config")
	}
	pluginCommandTest(t, 1, "config", "migrate", "--config", path, "--check")
	pluginCommandTest(t, 0, "config", "migrate", "--config", path, "--write")
	pluginCommandTest(t, 0, "config", "migrate", "--config", path, "--check")
	pluginCommandTest(t, 2, "config", "migrate", "--config", path, "--check", "--write")
}

func TestMigrationDefaultPluginsAfterEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		env         map[string]string
		ruff, shell bool
	}{
		{name: "ordinary check", ruff: true, shell: true},
		{name: "explicit ruff", args: []string{"--tool", "ruff"}, shell: true},
		{name: "disable ruff", args: []string{"--no-tool", "ruff"}, shell: true},
		{name: "empty ruff", args: []string{"--tool", "ruff="}, shell: true},
		{name: "false ruff", args: []string{"--tool", "ruff=false"}, shell: true},
		{name: "legacy pyflakes", args: []string{"--pyflakes=pyflakes"}, shell: true},
		{name: "disabled pyflakes", args: []string{"--pyflakes="}, shell: true},
		{name: "ruff executable env", env: map[string]string{"ACTIONLINT_TOOL_RUFF_BIN": "ruff"}, shell: true},
		{name: "ruff disabled env", env: map[string]string{"ACTIONLINT_TOOL_RUFF_BIN": ""}, shell: true},
		{name: "ruff arguments env", env: map[string]string{"ACTIONLINT_TOOL_RUFF_FLAGS": "--select F"}, shell: true},
		{name: "ruff child env", env: map[string]string{"ACTIONLINT_TOOL_RUFF_ENV": "NAME=value"}, shell: true},
		{name: "explicit shellcheck", args: []string{"--tool", "shellcheck"}, ruff: true},
		{name: "legacy shellcheck command", args: []string{"--shellcheck=shellcheck"}, ruff: true},
		{name: "disabled shellcheck", args: []string{"--no-tool", "shellcheck"}, ruff: true},
		{name: "legacy empty shellcheck", args: []string{"--shellcheck="}, ruff: true},
		{name: "shellcheck executable env", env: map[string]string{"ACTIONLINT_TOOL_SHELLCHECK_BIN": "shellcheck"}, ruff: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, tool := range []string{"TOOL_RUFF", "TOOL_SHELLCHECK"} {
				for _, suffix := range []string{"BIN", "FLAGS", "ENV"} {
					name := "ACTIONLINT_" + tool + "_" + suffix
					t.Setenv(name, "")
					if err := os.Unsetenv(name); err != nil {
						t.Fatal(err)
					}
				}
			}
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			app := newCommandApp(t.Context(), &Command{})
			if _, _, _, err := app.parseRoot(append(tc.args, "input.yml")); err != nil {
				t.Fatal(err)
			}
			if err := app.applyEnvironment(); err != nil {
				t.Fatal(err)
			}
			defaults := app.inv.Check.defaultPlugins(nil)
			ruff, shell := slices.Contains(defaults.Tools, "ruff"), slices.Contains(defaults.Tools, "shellcheck")
			if ruff != tc.ruff || shell != tc.shell {
				t.Fatalf("migration defaults: %v; want ruff=%t shellcheck=%t", defaults.Tools, tc.ruff, tc.shell)
			}
		})
	}
}
