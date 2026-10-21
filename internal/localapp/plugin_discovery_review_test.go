package localapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginConfigDiscoverySkipsDanglingLinks(t *testing.T) {
	root := pluginRepository(t)
	directory := filepath.Join(root, ".github")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "missing.yaml"), filepath.Join(directory, "actionlint.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(directory, "actionlint.yml")
	writeManifestTest(t, path, "plugins: []\n")
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(project.Manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(got, want) || !project.Manifest.Declared {
		t.Fatalf("lower-priority declarations ignored: %+v", project.Manifest)
	}
}

func TestPluginConfigDiscoveryPreservesLookupErrors(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "actionlint.yaml")
	if err := os.Symlink(path, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeManifestTest(t, filepath.Join(directory, "actionlint.yml"), "plugins: []\n")
	if _, err := pluginConfigPath(directory); err == nil {
		t.Fatal("configuration symlink loop ignored")
	}
}
