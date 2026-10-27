package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestInspectConfigErrorClassification(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"invalid YAML", "files: ["},
		{"invalid settings", "files: {typo: true}"},
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
