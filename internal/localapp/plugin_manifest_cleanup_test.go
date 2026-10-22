package localapp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginref"
)

func TestReadOnlyPluginManifestReplacementCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actionlint.yml")
	const original = "plugins: [old/plugin]\n"
	writeManifestTest(t, path, original)
	manifest, err := OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Error(err)
			return
		}
		for _, entry := range entries {
			if err := os.Chmod(filepath.Join(dir, entry.Name()), 0o600); err != nil {
				t.Error(err)
			}
		}
	})
	err = manifest.Save(t.Context(), []pluginref.Reference{manifestTestReference(t, "new/plugin")})
	if runtime.GOOS == "windows" {
		if err == nil {
			t.Fatal("replaced a read-only Windows destination")
		}
		if got := readManifestTest(t, path); got != original {
			t.Fatalf("failed replacement changed original: %q", got)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "actionlint.yml" {
		t.Fatalf("replacement left transaction debris: %v", entries)
	}
}
