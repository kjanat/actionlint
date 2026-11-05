// Package repositoryfixture creates filesystem layouts for repository discovery tests.
package repositoryfixture

import (
	"os"
	"path/filepath"
	"testing"
)

// Directory creates a temporary directory with an empty .git marker for discovery tests.
func Directory(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}
