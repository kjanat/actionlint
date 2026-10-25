package fsread

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotChildGrantRemainsWithinAuthorizedRoot(t *testing.T) {
	workspace, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "nested", "value"), []byte("allowed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	rootOpen := true
	t.Cleanup(func() {
		if rootOpen {
			if err := root.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	reader := New(t.Context(), nil, nil)
	for _, name := range []string{"escape", ".."} {
		snapshot, err := NewSnapshotAt(t.Context(), root, name, reader, nil)
		if err == nil {
			snapshot.Close()
			t.Fatalf("child grant escaped root: %q", name)
		}
	}
	snapshot, err := NewSnapshotAt(t.Context(), root, "nested", reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	rootOpen = false
	content, err := fs.ReadFile(snapshot, "value")
	if err != nil || string(content) != "allowed" {
		t.Fatalf("independent child grant: content=%q err=%v", content, err)
	}
}
