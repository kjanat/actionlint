package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestFileOverrideExplicitToolResets(t *testing.T) {
	t.Parallel()
	for _, overlay := range []string{
		"{shellcheck: {config: null}}",
		"{shellcheck: null}",
		"null",
	} {
		t.Run(overlay, func(t *testing.T) {
			t.Parallel()
			config, err := checking.ConfigurationRules().Parse([]byte("tools: {shellcheck: {enabled: false, config: .shellcheckrc}}\noverrides: [{includes: [ci.yml], tools: " + overlay + "}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				got, err := checking.ConfigurationRules().ForFile(config, "ci.yml", "")
				if err != nil || got.Tools["shellcheck"].Config != nil {
					t.Fatalf("explicit tool reset lost: %+v, %v", got, err)
				}
				if overlay == "{shellcheck: {config: null}}" && (got.Tools["shellcheck"].Enabled == nil || *got.Tools["shellcheck"].Enabled) {
					t.Fatal("config reset changed inherited tool enablement")
				}
				encoded, err := checking.ConfigurationRules().Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				config, err = checking.ConfigurationRules().Parse(encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestInspectConfigErrorClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, content string }{
		{"invalid YAML", "files: ["},
		{"invalid settings", "tools: {shellcheck: {enabled: wrong}}"},
	} {
		for _, inherited := range []bool{false, true} {
			t.Run(tc.name+"/"+map[bool]string{false: "selected", true: "inherited"}[inherited], func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				path := writeOverrideFixture(t, root, "invalid.yml", tc.content)
				if inherited {
					path = writeOverrideFixture(t, root, "actionlint.yml", "extends: [invalid.yml]\n")
				}
				_, want := checking.ConfigurationRules().Load(path)
				got, err := checking.ConfigurationRules().Inspect(repoconfig.Selection{Path: path}, true)
				if want == nil || err == nil || err.Error() != want.Error() || !strings.Contains(err.Error(), "could not parse config file") || strings.Contains(err.Error(), "could not read config file") || got.Path != path {
					t.Fatalf("inspection=%+v error=%v, want %v", got, err, want)
				}
			})
		}
	}
	path := filepath.Join(t.TempDir(), "missing.yml")
	_, want := checking.ConfigurationRules().Load(path)
	_, err := checking.ConfigurationRules().Inspect(repoconfig.Selection{Path: path}, true)
	if err == nil || err.Error() != want.Error() || strings.Count(err.Error(), "could not read config file") != 1 {
		t.Fatalf("read error=%v, want %v", err, want)
	}
}
