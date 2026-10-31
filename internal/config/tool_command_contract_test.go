package config_test

import (
	"os"
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestInheritedBackendExecutableOrigins(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent")
	if err := os.Mkdir(parentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(parentDir, "base.yaml")
	if err := os.WriteFile(parent, []byte(`tools:
  ruff:
    backend:
      process: {executable: ./bin/ruff}
overrides:
  - includes: ['*.yml']
    tools:
      shellcheck:
        backend:
          process: {executable: ./bin/shellcheck}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "actionlint.yaml")
	if err := os.WriteFile(child, []byte("extends: [./parent/base.yaml]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := repoconfig.Load(child)
	if err != nil {
		t.Fatal(err)
	}
	if command, _ := cfg.Tools["ruff"].Backend.Process(); command.Executable() != filepath.Join(parentDir, "bin", "ruff") {
		t.Fatal(command.Executable())
	}
	effective, err := repoconfig.ForFile(cfg, filepath.Join(root, "ci.yml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if command, _ := effective.Tools["shellcheck"].Backend.Process(); command.Executable() != filepath.Join(parentDir, "bin", "shellcheck") {
		t.Fatal(command.Executable())
	}
}
