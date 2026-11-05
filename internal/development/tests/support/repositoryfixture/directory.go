// Package repositoryfixture creates filesystem layouts for repository discovery tests.
package repositoryfixture

import (
	"os"
	"path/filepath"
	"testing"
)

// Directory creates a temporary directory with an empty .git marker for discovery tests.
func Directory(tb testing.TB) string {
	tb.Helper()
	root := tb.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		tb.Fatal(err)
	}
	return root
}
