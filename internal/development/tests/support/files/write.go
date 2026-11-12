// Package files writes source files used by tests.
package files

import (
	"os"
	"path/filepath"
	"testing"
)

// Write joins root and name, creates parent directories, writes content and returns the file path.
func Write(tb testing.TB, root, name, content string) string {
	tb.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

// WriteText writes content to an existing directory with owner-only file permissions.
func WriteText(tb testing.TB, path, content string) {
	tb.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
}
