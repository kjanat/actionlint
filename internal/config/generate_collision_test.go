package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestGeneratePreservesExistingConfig(t *testing.T) {
	for _, name := range repoconfig.RepositoryConfigNames {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := writeOverrideFixture(t, root, filepath.Join(".github", name), "# existing configuration\n")
			_, err := repoconfig.Generate(root, true, "test")
			if err == nil || !strings.Contains(err.Error(), "config file already exists at") {
				t.Fatalf("wrong collision error: %v", err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "# existing configuration\n" {
				t.Fatalf("existing config changed: %q", content)
			}
			entries, err := os.ReadDir(filepath.Join(root, ".github"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("configuration created beside existing file: %v, %v", entries, err)
			}
		})
	}
}
