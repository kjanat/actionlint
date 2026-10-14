package fsread

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"a/deep", "b", "skip/inner"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"a/deep/x.txt", "b/y.txt", "skip/inner/z.txt", "top.txt"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestReaderRecordsSuccessfulReads(t *testing.T) {
	root := tree(t)
	var recorded []string
	reader := New(t.Context(), nil, func(path string) { recorded = append(recorded, path) })
	file := filepath.Join(root, "top.txt")
	if data, err := reader.ReadFile(file); err != nil || string(data) != "top.txt" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if _, err := reader.ReadFile(filepath.Join(root, "absent")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile of an absent file = %v", err)
	}
	opened, err := reader.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Open(filepath.Join(root, "absent")); err == nil {
		t.Fatal("Open of an absent file succeeded")
	}
	if entries, err := reader.ReadDir(root); err != nil || len(entries) != 4 {
		t.Fatalf("ReadDir = %v, %v", entries, err)
	}
	if _, err := reader.ReadDir(filepath.Join(root, "absent")); err == nil {
		t.Fatal("ReadDir of an absent directory succeeded")
	}
	reader.Record("stdin.yml")
	if want := []string{file, file, root, "stdin.yml"}; !slices.Equal(recorded, want) {
		t.Fatalf("recorded %q, want %q", recorded, want)
	}
}

func TestReaderUsesContents(t *testing.T) {
	var recorded []string
	reader := New(t.Context(), func(path string) ([]byte, error) {
		if path == "virtual" {
			return []byte("in memory"), nil
		}
		return nil, fs.ErrNotExist
	}, func(path string) { recorded = append(recorded, path) })
	if data, err := reader.ReadFile("virtual"); err != nil || string(data) != "in memory" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if _, err := reader.ReadFile("other"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile of a missing virtual file = %v", err)
	}
	if !slices.Equal(recorded, []string{"virtual"}) {
		t.Fatalf("recorded %q", recorded)
	}
}

func TestReaderWalkDir(t *testing.T) {
	root := tree(t)
	var recorded, visited []string
	reader := New(t.Context(), nil, func(path string) { recorded = append(recorded, path) })
	err := reader.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		visited = append(visited, filepath.ToSlash(relative))
		if entry.IsDir() && entry.Name() == "skip" {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		want = append(want, filepath.ToSlash(relative))
		if entry.IsDir() && entry.Name() == "skip" {
			return filepath.SkipDir
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(visited, want) {
		t.Fatalf("visited %q, filepath.WalkDir visits %q", visited, want)
	}
	wantRecorded := []string{root, filepath.Join(root, "a"), filepath.Join(root, "a", "deep"), filepath.Join(root, "b")}
	if !slices.Equal(recorded, wantRecorded) {
		t.Fatalf("recorded %q, want %q", recorded, wantRecorded)
	}
	recorded, visited = nil, nil
	if err := reader.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		visited = append(visited, path)
		if path == filepath.Join(root, "a") {
			return filepath.SkipAll
		}
		return err
	}); err != nil || len(visited) != 2 || !slices.Equal(recorded, []string{root}) {
		t.Fatalf("SkipAll walk = %v, visited %q, recorded %q", err, visited, recorded)
	}
	recorded = nil
	absent := filepath.Join(root, "absent")
	if err := reader.WalkDir(absent, func(path string, entry fs.DirEntry, err error) error { return err }); !errors.Is(err, fs.ErrNotExist) || recorded != nil {
		t.Fatalf("walk of an absent root = %v, recorded %q", err, recorded)
	}
	file := filepath.Join(root, "top.txt")
	if err := reader.WalkDir(file, func(path string, entry fs.DirEntry, err error) error {
		visited = append(visited, path)
		return err
	}); err != nil || visited[len(visited)-1] != file || recorded != nil {
		t.Fatalf("walk of a file = %v, recorded %q", err, recorded)
	}
}

func TestReaderWalkDirUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced")
	}
	root := tree(t)
	locked := filepath.Join(root, "b")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	var recorded []string
	reader := New(t.Context(), nil, func(path string) { recorded = append(recorded, path) })
	err := reader.WalkDir(root, func(path string, entry fs.DirEntry, err error) error { return err })
	if !errors.Is(err, fs.ErrPermission) || slices.Contains(recorded, locked) {
		t.Fatalf("walk = %v, recorded %q", err, recorded)
	}
	recorded = nil
	if err := reader.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil && path == locked {
			return filepath.SkipDir
		}
		return err
	}); err != nil || slices.Contains(recorded, locked) || !slices.Contains(recorded, filepath.Join(root, "skip", "inner")) {
		t.Fatalf("walk skipping the unreadable directory = %v, recorded %q", err, recorded)
	}
}

func TestReaderMapped(t *testing.T) {
	root := tree(t)
	var recorded []string
	reader := New(t.Context(), nil, func(path string) { recorded = append(recorded, path) })
	mapped := reader.Mapped(func(path string) string { return strings.Replace(path, root, "logical", 1) })
	if _, err := mapped.ReadDir(root); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadDir(root); err != nil {
		t.Fatal(err)
	}
	if want := []string{"logical", root}; !slices.Equal(recorded, want) {
		t.Fatalf("recorded %q, want %q", recorded, want)
	}
}

func TestReaderCancelled(t *testing.T) {
	root := tree(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := New(ctx, nil, func(path string) { t.Errorf("recorded %s after cancellation", path) })
	_, readErr := reader.ReadFile(filepath.Join(root, "top.txt"))
	_, openErr := reader.Open(filepath.Join(root, "top.txt"))
	_, dirErr := reader.ReadDir(root)
	walkErr := reader.WalkDir(root, func(path string, entry fs.DirEntry, err error) error { return err })
	for name, err := range map[string]error{"ReadFile": readErr, "Open": openErr, "ReadDir": dirErr, "WalkDir": walkErr} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s = %v, want context.Canceled", name, err)
		}
	}
}

func TestReaderWalkStopsWhenCancelled(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	var visited []string
	err := New(ctx, nil, nil).WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		visited = append(visited, filepath.Base(path))
		if path != root {
			cancel()
		}
		return err
	})
	if !errors.Is(err, context.Canceled) || !slices.Equal(visited, []string{filepath.Base(root), "a"}) {
		t.Fatalf("walk = %v after visiting %q, want context.Canceled after a", err, visited)
	}
	file := filepath.Join(root, "a")
	if err := New(ctx, nil, nil).WalkDir(file, func(string, fs.DirEntry, error) error {
		t.Error("visited a file root after cancellation")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("walk of a file root = %v, want context.Canceled", err)
	}
}

func TestUnrecordedHelpers(t *testing.T) {
	root := tree(t)
	if data, err := Disk(filepath.Join(root, "top.txt")); err != nil || string(data) != "top.txt" {
		t.Fatalf("Disk = %q, %v", data, err)
	}
	if entries, err := ProbeDir(root); err != nil || len(entries) != 4 {
		t.Fatalf("ProbeDir = %v, %v", entries, err)
	}
	reader := New(t.Context(), nil, nil)
	if _, err := reader.ReadDir(root); err != nil {
		t.Fatalf("a reader without a recorder failed: %v", err)
	}
}
