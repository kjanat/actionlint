// Package fixturefiles writes source files used by tests.
package fixturefiles

import (
	"os"
	"path/filepath"
	"testing"
)

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

func WriteText(tb testing.TB, path, content string) {
	tb.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
}
