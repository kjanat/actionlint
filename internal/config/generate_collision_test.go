package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

func TestGeneratePreservesExistingConfig(t *testing.T) {
	for _, name := range []string{"actionlint.yaml", "actionlint.yml"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := writeOverrideFixture(t, root, filepath.Join(".github", name), "# existing configuration\n")
			_, err := repoconfig.Generate(root, false, "test")
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
		})
	}
}
