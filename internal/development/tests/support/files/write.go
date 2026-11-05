// Package files writes source files used by tests.
package files

import (
	"os"
	"path/filepath"
	"testing"
)

// Write joins root and name, creates parent directories, writes content and returns the file path.
func Write(t testing.TB, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// WriteText writes content to an existing directory with owner-only file permissions.
func WriteText(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
