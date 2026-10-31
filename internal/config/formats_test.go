package config

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var formatSources = map[string]string{
	"actionlint.yaml": "self-hosted-runner:\n  labels: [gpu]\nconfig-variables: [STAGE]\n",
	"actionlint.yml":  "self-hosted-runner:\n  labels: [gpu]\nconfig-variables: [STAGE]\n",
	"actionlint.json": "{\n\t\"$schema\": \"../actionlint.schema.json\",\n\t\"self-hosted-runner\": {\"labels\": [\"gpu\"]},\n\t\"config-variables\": [\"STAGE\"]\n}\n",
	"actionlint.toml": "config-variables = [\"STAGE\"]\n\n[self-hosted-runner]\nlabels = [\"gpu\"]\n",
}

func writeConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadConfigFormats(t *testing.T) {
	for name, source := range formatSources {
		t.Run(name, func(t *testing.T) {
			loaded, err := Read(writeConfig(t, t.TempDir(), name, source), os.ReadFile)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff([]string{"gpu"}, RunnerLabels(loaded.Config.Facts())); diff != "" {
				t.Fatal(diff)
			}
			if diff := cmp.Diff([]string{"STAGE"}, VariableNames(loaded.Config.Facts())); diff != "" {
				t.Fatal(diff)
			}
			if len(loaded.Warnings) != 0 {
				t.Fatalf("warnings: %+v", loaded.Warnings)
			}
		})
	}
}

func TestLoadRepositoryFindsEveryFormat(t *testing.T) {
	for _, name := range RepositoryConfigNames {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			github := filepath.Join(root, ".github")
			if err := os.Mkdir(github, 0o755); err != nil {
				t.Fatal(err)
			}
			want := writeConfig(t, github, name, formatSources[name])
			loaded, path, err := LoadRepository(root, os.ReadFile)
			if err != nil {
				t.Fatal(err)
			}
			if path != want {
				t.Fatalf("path=%q want %q", path, want)
			}
			if diff := cmp.Diff([]string{"gpu"}, RunnerLabels(loaded.Config.Facts())); diff != "" {
				t.Fatal(diff)
			}
		})
	}
	root := t.TempDir()
	github := filepath.Join(root, ".github")
	if err := os.Mkdir(github, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, github, "actionlint.toml", "config-variables = [\"TOML\"]\n")
	writeConfig(t, github, "actionlint.yml", "config-variables: [YAML]\n")
	loaded, path, err := LoadRepository(root, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "actionlint.yml" || VariableNames(loaded.Config.Facts())[0] != "YAML" {
		t.Fatalf("path=%q variables=%v", path, VariableNames(loaded.Config.Facts()))
	}
}

func TestInspectFindsEveryRepositoryFormat(t *testing.T) {
	for _, name := range RepositoryConfigNames {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			github := filepath.Join(root, ".github")
			if err := os.Mkdir(github, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			want := writeConfig(t, github, name, formatSources[name])
			t.Chdir(github)
			got, err := Inspect(Selection{}, true)
			if err != nil {
				t.Fatal(err)
			}
			if got.Path != want {
				t.Fatalf("selected path=%q, want %q", got.Path, want)
			}
			settings := got.Config
			for _, key := range []string{"repository"} {
				var ok bool
				settings, ok = settings[key].(map[string]any)
				if !ok {
					t.Fatalf("missing canonical configuration field %s", key)
				}
			}
			variables, err := jsonv2.Marshal(settings["variables"])
			if err != nil || string(variables) != `{"STAGE":null}` {
				t.Fatalf("configuration variables=%s, error=%v", variables, err)
			}
		})
	}
}

func TestTOMLConfigReportsPositions(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(writeConfig(t, dir, "actionlint.toml", "config-variables = [\"A\"]\n\n[self-hosted-runner]\nlabels = 7\n"))
	if err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("type error: %v", err)
	}
	_, err = Load(writeConfig(t, dir, "broken.toml", "config-variables = [\"A\"]\nconfig-variables = [\"B\"]\n"))
	if err == nil || !strings.Contains(err.Error(), `line 2, column 1: key "config-variables" defined twice`) {
		t.Fatalf("duplicate key: %v", err)
	}
	loaded, err := Read(writeConfig(t, dir, "warn.toml", "[self-hosted-runner]\nlabel = [\"x\"]\n"), os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Warnings) != 1 || loaded.Warnings[0].Line != 2 || loaded.Warnings[0].Column != 1 || !strings.Contains(loaded.Warnings[0].Message, `"self-hosted-runner.label"`) {
		t.Fatalf("warnings: %+v", loaded.Warnings)
	}
}

func TestExtendsAcrossFormats(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "base.json", "{\"self-hosted-runner\": {\"labels\": [\"gpu\"]}}\n")
	writeConfig(t, dir, "middle.toml", "extends = [\"base.json\"]\nconfig-secrets = [\"TOKEN\"]\n")
	path := writeConfig(t, dir, "actionlint.yaml", "extends: [middle.toml]\nconfig-variables: [STAGE]\n")
	loaded, err := Read(path, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := loaded.Config
	if diff := cmp.Diff([]string{"gpu"}, RunnerLabels(cfg.Facts())); diff != "" {
		t.Fatal(diff)
	}
	if diff := cmp.Diff([]string{"TOKEN"}, SecretNames(cfg.Facts())); diff != "" {
		t.Fatal(diff)
	}
	if diff := cmp.Diff([]string{"STAGE"}, VariableNames(cfg.Facts())); diff != "" {
		t.Fatal(diff)
	}
	if len(cfg.Sources()) != 3 {
		t.Fatalf("sources: %v", cfg.Sources())
	}
}
