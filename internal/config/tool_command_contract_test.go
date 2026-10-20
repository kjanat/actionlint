package config_test

import (
	"os"
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
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
	if got := cfg.Tools.Ruff.Backend.Process.Executable; got != filepath.Join(parentDir, "bin", "ruff") {
		t.Fatal(got)
	}
	effective, err := repoconfig.ForFile(cfg, filepath.Join(root, "ci.yml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := effective.Tools.Shellcheck.Backend.Process.Executable; got != filepath.Join(parentDir, "bin", "shellcheck") {
		t.Fatal(got)
	}
}
