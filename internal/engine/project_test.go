package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// Project discovery needs a .git marker. Keep it in a temporary fixture copy so
// tests also work without a checkout and editors do not discover fake repositories.
func testProjectDir(t *testing.T, fixture string) string {
	t.Helper()
	d := t.TempDir()
	if err := os.CopyFS(d, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(d, ".git"), 0750); err != nil {
		t.Fatal(err)
	}
	return d
}
