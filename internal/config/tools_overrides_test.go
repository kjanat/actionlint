package config_test

import (
	"os"
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestFileOverrideToolSettings(t *testing.T) {
	config, err := repoconfig.Parse([]byte(`tools:
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
		got, err := repoconfig.ForFile(config, "action.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Tools.Shellcheck.Enabled == nil || *got.Tools.Shellcheck.Enabled {
			t.Fatalf("switch was lost: %+v", got.Tools)
		}
		encoded, err := codec.Marshal(got.Tools)
		if err != nil {
			t.Fatal(err)
		}
		rc := got.Tools.Shellcheck.Config.Inline()
		if rc == nil || len(rc.Disable) != 1 || rc.Disable[0] != "SC2086" || len(rc.Enable) != 1 || rc.Enable[0] != "all" {
			t.Fatalf("settings lost: %s", encoded)
		}
		reset, err := repoconfig.ForFile(config, "reset.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if reset.Tools.Shellcheck.Enabled != nil || reset.Tools.Shellcheck.Config != nil {
			t.Fatalf("reset lost: %+v", reset.Tools)
		}
		if config.Tools.Shellcheck.Enabled == nil || !*config.Tools.Shellcheck.Enabled {
			t.Fatal("mutated parent tool config")
		}
		encoded, err = codec.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		config, err = repoconfig.Parse(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileOverrideInheritedToolConfigPath(t *testing.T) {
	root := t.TempDir()
	writeOverrideFixture(t, root, "base/base.yml", "overrides: [{includes: [ci.yml], tools: {shellcheck: {config: .shellcheckrc}}}]")
	path := writeOverrideFixture(t, root, "project/actionlint.yml", "extends: [../base/base.yml]")
	config, err := repoconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repoconfig.ForFile(config, "ci.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "base", ".shellcheckrc"); got.Tools.Shellcheck.Config.Path() != want {
		t.Fatalf("got %q; want declaring-config-relative %q", got.Tools.Shellcheck.Config.Path(), want)
	}
}

func TestFileOverrideRejectsInvalidTools(t *testing.T) {
	for _, settings := range []string{"{typo: true}", "{shellcheck: {enabled: wrong}}", "{shellcheck: {config: {typo: true}}}"} {
		if _, err := repoconfig.Parse([]byte("overrides: [{includes: [ci.yml], tools: " + settings + "}]")); err == nil {
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
