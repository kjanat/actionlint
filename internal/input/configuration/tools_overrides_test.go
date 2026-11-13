package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

func TestFileOverrideToolSettings(t *testing.T) {
	t.Parallel()
	config, err := checking.ConfigurationRules().Parse([]byte(`tools:
  shellcheck: {enabled: true, config: {disable: [SC2086]}}
overrides:
  - includes: [action.yml]
    tools: {shellcheck: false}
  - includes: [action.yml]
    tools: {shellcheck: {config: {enable: [all]}}}
  - includes: [reset.yml]
    tools: null
`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := checking.ConfigurationRules().ForFile(config, "action.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Tools["shellcheck"].Enabled == nil || *got.Tools["shellcheck"].Enabled {
			t.Fatalf("switch was lost: %+v", got.Tools)
		}
		encoded, err := codec.Marshal(got.Tools)
		if err != nil {
			t.Fatal(err)
		}
		if rc := got.Tools["shellcheck"].Config.Inline(); !reflect.DeepEqual(rc, map[string]any{"disable": []any{"SC2086"}, "enable": []any{"all"}}) {
			t.Fatalf("settings lost: %s", encoded)
		}
		reset, err := checking.ConfigurationRules().ForFile(config, "reset.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if reset.Tools["shellcheck"].Enabled != nil || reset.Tools["shellcheck"].Config != nil {
			t.Fatalf("reset lost: %+v", reset.Tools)
		}
		if config.Tools["shellcheck"].Enabled == nil || !*config.Tools["shellcheck"].Enabled {
			t.Fatal("mutated parent tool config")
		}
		encoded, err = checking.ConfigurationRules().Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		config, err = checking.ConfigurationRules().Parse(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileOverrideInheritedToolConfigPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeOverrideFixture(t, root, "base/base.yml", "overrides: [{includes: [ci.yml], tools: {shellcheck: {config: .shellcheckrc}}}]")
	path := writeOverrideFixture(t, root, "project/actionlint.yml", "extends: [../base/base.yml]")
	config, err := checking.ConfigurationRules().Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := checking.ConfigurationRules().ForFile(config, "ci.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "base", ".shellcheckrc"); got.Tools["shellcheck"].Config.Path() != want {
		t.Fatalf("got %q; want declaring-config-relative %q", got.Tools["shellcheck"].Config.Path(), want)
	}
}

func TestFileOverrideRejectsInvalidTools(t *testing.T) {
	t.Parallel()
	for _, settings := range []string{"{typo: true}", "{shellcheck: {enabled: wrong}}", "{shellcheck: {config: {typo: true}}}"} {
		if _, err := checking.ConfigurationRules().Parse([]byte("overrides: [{includes: [ci.yml], tools: " + settings + "}]")); err == nil {
			t.Errorf("accepted invalid override tool settings: %s", settings)
		}
	}
}

func writeOverrideFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
