package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"github.com/tidwall/jsonc"
)

func TestConfigInitFormats(t *testing.T) {
	for _, format := range repoconfig.FileFormats() {
		t.Run(string(format), func(t *testing.T) {
			fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
			t.Chdir(commandTestRepo(t))
			got := testRunCommand("", "config", "init", "--"+string(format))
			if got.Status != 0 || !strings.Contains(got.Stdout, format.Filename()) {
				t.Fatal(got)
			}
			path := filepath.Join(".github", format.Filename())
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if format == repoconfig.FormatJSON && !json.Valid(content) {
				t.Fatalf("invalid JSON:\n%s", content)
			}
			if format == repoconfig.FormatJSONC && (!json.Valid(jsonc.ToJSON(content)) || !strings.Contains(string(content), "// Rule defaults")) {
				t.Fatalf("missing JSONC reference:\n%s", content)
			}
			cfg, err := checking.ConfigurationRules().Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Plugins) != 2 || cfg.Lint.Rules.Preset != "recommended" {
				t.Fatalf("wrong defaults: %+v", cfg)
			}
			selected := testRunCommand("", "config", "path")
			if selected.Status != 0 || !strings.Contains(selected.Stdout, format.Filename()) {
				t.Fatal(selected)
			}
			for _, other := range repoconfig.FileFormats() {
				got := testRunCommand("", "config", "init", "--"+string(other))
				if got.Status != 3 || !strings.Contains(got.Stdout+got.Stderr, "already exists") {
					t.Fatalf("overwrote %s with %s: %+v", format, other, got)
				}
			}
			actual, err := os.ReadFile(path)
			if err != nil || string(actual) != string(content) {
				t.Fatalf("existing configuration changed: %v", err)
			}
		})
	}
}

func TestConfigInitFormatSelection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flags    []string
		filename string
	}{
		{"repeated", []string{"--json", "--json"}, "actionlint.json"},
		{"false", []string{"--json=false"}, "actionlint.yaml"},
		{"reset", []string{"--json", "--json=false", "--toml"}, "actionlint.toml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
			t.Chdir(commandTestRepo(t))
			got := testRunCommand("", append([]string{"config", "init"}, tc.flags...)...)
			if got.Status != 0 {
				t.Fatal(got)
			}
			if _, err := os.Stat(filepath.Join(".github", tc.filename)); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, first := range repoconfig.FileFormats() {
		for _, second := range repoconfig.FileFormats() {
			if first == second {
				continue
			}
			t.Run(string(first)+"/"+string(second), func(t *testing.T) {
				t.Chdir(commandTestRepo(t))
				got := testRunCommand("", "config", "init", "--"+string(first), "--"+string(second))
				if got.Status != 2 || !strings.Contains(got.Stdout+got.Stderr, "cannot be combined") {
					t.Fatal(got)
				}
				for _, name := range repoconfig.RepositoryConfigNames {
					if _, err := os.Stat(filepath.Join(".github", name)); !os.IsNotExist(err) {
						t.Fatalf("conflicting flags created %s: %v", name, err)
					}
				}
			})
		}
	}
}

func TestConfigInitFormatHelp(t *testing.T) {
	t.Parallel()
	got := testRunCommand("", "config", "init", "--help")
	if got.Status != 0 {
		t.Fatal(got)
	}
	for _, text := range []string{"--json", "--jsonc", "--toml", "--yaml", "--yml", "actionlint config init --toml"} {
		if !strings.Contains(got.Stdout+got.Stderr, text) {
			t.Errorf("help omits %q: %+v", text, got)
		}
	}
}

func TestConfigInitJSONReceipt(t *testing.T) {
	fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	t.Chdir(commandTestRepo(t))
	got := testRunCommand("", "--json", "config", "init", "--toml")
	if got.Status != 0 {
		t.Fatal(got)
	}
	var receipt struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &receipt); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(receipt.Path) || filepath.Base(receipt.Path) != "actionlint.toml" {
		t.Fatalf("incorrect path: %q", receipt.Path)
	}
	if _, err := checking.ConfigurationRules().Load(receipt.Path); err != nil {
		t.Fatal(err)
	}
}
