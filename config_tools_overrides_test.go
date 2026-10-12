package actionlint

import (
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestFileOverrideToolSettings(t *testing.T) {
	config, err := ParseConfig([]byte(`tools:
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
		got, err := configForFile(config, "action.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Tools.Shellcheck.Enabled == nil || *got.Tools.Shellcheck.Enabled {
			t.Fatalf("switch was lost: %+v", got.Tools)
		}
		encoded, err := yaml.Marshal(got.Tools)
		if err != nil {
			t.Fatal(err)
		}
		rc := got.Tools.Shellcheck.Config.inline()
		if rc == nil || len(rc.Disable) != 1 || rc.Disable[0] != "SC2086" || len(rc.Enable) != 1 || rc.Enable[0] != "all" {
			t.Fatalf("settings lost: %s", encoded)
		}
		reset, err := configForFile(config, "reset.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if reset.Tools.Shellcheck.Enabled != nil || reset.Tools.Shellcheck.Config != nil {
			t.Fatalf("reset lost: %+v", reset.Tools)
		}
		if config.Tools.Shellcheck.Enabled == nil || !*config.Tools.Shellcheck.Enabled {
			t.Fatal("mutated parent tool config")
		}
		encoded, err = yaml.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		config, err = ParseConfig(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileOverrideToolConfigOrigins(t *testing.T) {
	for _, content := range []string{
		"tools: {shellcheck: {config: .shellcheckrc}}\noverrides: [{includes: [ci.yml], tools: {shellcheck: false}}]",
		"overrides: [{includes: [ci.yml], tools: {shellcheck: {config: .shellcheckrc}}}]",
	} {
		t.Run(content, func(t *testing.T) {
			overlay, err := ParseConfigOverlay("config", []byte(content))
			if err != nil {
				t.Fatal(err)
			}
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: t.TempDir(), ConfigOverlays: []ConfigOverlay{overlay}})
			if err != nil {
				t.Fatal(err)
			}
			config, err := session.configForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := configForFile(config, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			if got.Tools.Shellcheck.Config == nil || !got.Tools.Shellcheck.Config.fromInput {
				t.Fatal("lost working-directory origin for input-supplied tool configuration")
			}
		})
	}
}

func TestFileOverrideInheritedToolConfigPath(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "base/base.yml", "overrides: [{includes: [ci.yml], tools: {shellcheck: {config: .shellcheckrc}}}]")
	path := writeShellcheckFixture(t, root, "project/actionlint.yml", "extends: [../base/base.yml]")
	config, err := ReadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := configForFile(config, "ci.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "base", ".shellcheckrc"); got.Tools.Shellcheck.Config.path() != want {
		t.Fatalf("got %q; want declaring-config-relative %q", got.Tools.Shellcheck.Config.path(), want)
	}
}

func TestFileOverrideToolRequirements(t *testing.T) {
	root := t.TempDir()
	config := writeShellcheckFixture(t, root, "actionlint.yml", "tools: {shellcheck: false}\noverrides: [{includes: [ci.yml], tools: {shellcheck: true}}]")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, ConfigFile: config, Shellcheck: "shellcheck"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{{"ci.yml", true}, {"other.yml", false}} {
		got, err := session.RequiredTools([]string{tc.path})
		if err != nil || got.Shellcheck != tc.want {
			t.Fatalf("%s: got %+v, %v; want shellcheck=%v", tc.path, got, err, tc.want)
		}
	}
}

func TestFileOverrideRejectsInvalidTools(t *testing.T) {
	for _, settings := range []string{"{typo: true}", "{shellcheck: {enabled: wrong}}", "{shellcheck: {config: {typo: true}}}"} {
		if _, err := ParseConfig([]byte("overrides: [{includes: [ci.yml], tools: " + settings + "}]")); err == nil {
			t.Errorf("accepted invalid override tool settings: %s", settings)
		}
	}
}
