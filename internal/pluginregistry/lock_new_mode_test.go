package pluginregistry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLockFileMode(t *testing.T) {
	dir := t.TempDir()
	control := filepath.Join(dir, "control")
	if err := os.WriteFile(control, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(control)
	if err != nil {
		t.Fatal(err)
	}
	file, err := OpenLock(t.Context(), filepath.Join(dir, LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file.Path)
	if err != nil || info.Mode().Perm() != want.Mode().Perm() {
		t.Fatalf("new lock mode=%v, want %o: %v", info, want.Mode().Perm(), err)
	}
}
