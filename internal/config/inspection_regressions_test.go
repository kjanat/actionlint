package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestFileOverrideExplicitToolResets(t *testing.T) {
	for _, overlay := range []string{
		"{shellcheck: {config: null}}",
		"{shellcheck: null}",
		"null",
	} {
		t.Run(overlay, func(t *testing.T) {
			config, err := repoconfig.Parse([]byte("tools: {shellcheck: {enabled: false, config: .shellcheckrc}}\noverrides: [{includes: [ci.yml], tools: " + overlay + "}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				got, err := repoconfig.ForFile(config, "ci.yml", "")
				if err != nil || got.Tools["shellcheck"].Config != nil {
					t.Fatalf("explicit tool reset lost: %+v, %v", got, err)
				}
				if overlay == "{shellcheck: {config: null}}" && (got.Tools["shellcheck"].Enabled == nil || *got.Tools["shellcheck"].Enabled) {
					t.Fatal("config reset changed inherited tool enablement")
				}
				encoded, err := codec.Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				config, err = repoconfig.Parse(encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestInspectConfigErrorClassification(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"invalid YAML", "files: ["},
		{"invalid settings", "tools: {shellcheck: {enabled: wrong}}"},
	} {
		for _, inherited := range []bool{false, true} {
			t.Run(tc.name+"/"+map[bool]string{false: "selected", true: "inherited"}[inherited], func(t *testing.T) {
				root := t.TempDir()
				path := writeOverrideFixture(t, root, "invalid.yml", tc.content)
				if inherited {
					path = writeOverrideFixture(t, root, "actionlint.yml", "extends: [invalid.yml]\n")
				}
				_, want := repoconfig.Load(path)
				got, err := repoconfig.Inspect(repoconfig.Selection{Path: path}, true)
				if want == nil || err == nil || err.Error() != want.Error() || !strings.Contains(err.Error(), "could not parse config file") || strings.Contains(err.Error(), "could not read config file") || got.Path != path {
					t.Fatalf("inspection=%+v error=%v, want %v", got, err, want)
				}
			})
		}
	}
	path := filepath.Join(t.TempDir(), "missing.yml")
	_, want := repoconfig.Load(path)
	_, err := repoconfig.Inspect(repoconfig.Selection{Path: path}, true)
	if err == nil || err.Error() != want.Error() || strings.Count(err.Error(), "could not read config file") != 1 {
		t.Fatalf("read error=%v, want %v", err, want)
	}
}
