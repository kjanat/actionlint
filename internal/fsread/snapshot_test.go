package fsread

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSnapshotReadOnlyAndStable(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "source.sh")
	if err := os.WriteFile(file, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	var inputs []string
	snapshot, err := NewSnapshot(t.Context(), root, New(t.Context(), nil, func(path string) { inputs = append(inputs, path) }), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := snapshot.Close(); err != nil {
			t.Error(err)
		}
	})
	dir, err := snapshot.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	open, ok := dir.(interface {
		OpenAt(string, int, fs.FileMode) (fs.File, error)
	})
	if !ok {
		t.Fatal("directory lacks relative open")
	}
	for _, flag := range []int{os.O_WRONLY, os.O_RDWR, os.O_CREATE, os.O_TRUNC} {
		if _, err := open.OpenAt("source.sh", flag, 0o600); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("write allowed: %v", err)
		}
	}
	for i := range 2 {
		f, err := open.OpenAt("source.sh", os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(f)
		if err != nil || string(data) != "original" {
			t.Fatalf("read %d: %q, %v", i, data, err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Contains(inputs, file) {
		t.Fatalf("unrecorded read: %v", inputs)
	}
}

func TestSnapshotRejectsEscapesAndLimits(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	snapshot, err := NewSnapshot(t.Context(), root, New(t.Context(), nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for _, name := range []string{"../secret", "/secret", "folder/../../secret"} {
		if _, err := snapshot.Open(name); err == nil {
			t.Fatalf("escaped: %s", name)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Open("link"); err == nil {
		t.Fatal("symlink escaped root")
	}
	if err := os.WriteFile(filepath.Join(root, "large"), bytes.Repeat([]byte("x"), (8<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Open("large"); err == nil {
		t.Fatal("source read exceeded budget")
	}
}

func TestSnapshotResolvesSymlinksBeforeParents(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"helper.sh": "wrong", "real/helper.sh": "correct"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join("real", "sub"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	var inputs []string
	snapshot, err := NewSnapshot(t.Context(), root, New(t.Context(), nil, func(name string) { inputs = append(inputs, name) }), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	dir, err := snapshot.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	open, ok := dir.(interface {
		OpenAt(string, int, fs.FileMode) (fs.File, error)
	})
	if !ok {
		t.Fatal("directory lacks relative open")
	}
	f, err := open.OpenAt("link/../helper.sh", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "correct" {
		t.Fatalf("source=%q error=%v", data, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	name, content, ok := snapshot.Content("link/../helper.sh")
	if !ok || name != "real/helper.sh" || string(content) != "correct" {
		t.Fatalf("snapshot=%q %q %v", name, content, ok)
	}
	for _, dependency := range []string{"link", "real/helper.sh"} {
		if !slices.Contains(inputs, filepath.Join(root, dependency)) {
			t.Fatalf("unrecorded dependency %s: %v", dependency, inputs)
		}
	}
	if slices.Contains(inputs, filepath.Join(root, "helper.sh")) {
		t.Fatalf("recorded wrong file: %v", inputs)
	}
}

func TestSnapshotBudgetIsSharedAndCannotBeIgnored(t *testing.T) {
	budget := &SnapshotBudget{}
	var snapshots []*SnapshotFS
	for range 2 {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "source.sh"), bytes.Repeat([]byte("x"), 5<<20), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := NewSnapshot(t.Context(), root, New(t.Context(), nil, nil), budget)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, snapshot)
	}
	f, err := snapshots[0].Open("source.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshots[1].Open("source.sh"); err == nil {
		t.Fatal("separate grant bypassed the shared byte limit")
	}
	for _, snapshot := range snapshots {
		if err := snapshot.Close(); err == nil {
			t.Fatal("ignored guest read error was not reported on close")
		}
	}
}
