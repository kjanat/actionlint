// Package repositoryfixture creates filesystem layouts for repository discovery tests.
package repositoryfixture

import (
	"os"
	"path/filepath"
	"testing"
)

func Directory(tb testing.TB) string {
	tb.Helper()
	root := tb.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		tb.Fatal(err)
	}
	return root
}
