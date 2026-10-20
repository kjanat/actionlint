package config_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/internal/yaml"
)

func TestFilesSelection(t *testing.T) {
	for _, tc := range []struct {
		setting string
		want    bool
	}{
		{"{}", true}, {"{includes: []}", false}, {"{includes: null}", true},
		{"{includes: ['**/*.yml']}", true}, {"{includes: ['**/*.yaml']}", false},
		{"{includes: ['**/*.yml'], excludes: ['**/ci.yml']}", false},
		{"{includes: ['!**/ci.yml', '**/*.yml']}", false},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte("files: " + tc.setting))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: ".github/workflows/ci.yml", Content: []byte("bad yaml: ["), Config: cfg}})
				if err != nil {
					t.Fatal(err)
				}
				if (len(result.Diagnostics) > 0) != tc.want || (result.FileCount > 0) != tc.want {
					t.Fatal(result)
				}
				b, err := yaml.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				cfg, err = repoconfig.Parse(b)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	for _, setting := range []string{"{includes: ['../*']}", "{includes: [1]}", "{excludes: ['[']}", "{exclude: []}"} {
		if _, err := repoconfig.Parse([]byte("files: " + setting)); err == nil {
			t.Fatalf("accepted %s", setting)
		}
	}
}
