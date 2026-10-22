package config_test

import (
	"os"
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestRepositoryRelativeConfigAliasBoundaries(t *testing.T) {
	root := t.TempDir()
	physical := writeOverrideFixture(t, root, "physical/ci.yml", "on: push\n")
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(filepath.Dir(physical), alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, tc := range []struct{ path, want string }{
		{filepath.Join(alias, "ci.yml"), filepath.Join("alias", "ci.yml")},
		{filepath.Join(root, "virtual", "ci.yml"), filepath.Join("virtual", "ci.yml")},
		{filepath.Join(root, "..", "missing", "ci.yml"), filepath.Join("..", "missing", "ci.yml")},
		{"./ci.yml", "./ci.yml"},
	} {
		if got := repoconfig.RepositoryRelativePath(tc.path, root); got != tc.want {
			t.Fatalf("path %q: got %q, want %q", tc.path, got, tc.want)
		}
	}
}
