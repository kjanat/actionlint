package files

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRootedReader(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, name := range []string{"z", "a"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var recorded []string
	reader := NewRooted(t.Context(), root, func(name string) { recorded = append(recorded, name) }).Mapped(func(name string) string { return "cache/" + name })
	content, err := reader.ReadFile("a")
	if err != nil || string(content) != "a" {
		t.Fatalf("read=%q error=%v", content, err)
	}
	file, err := reader.Open("a")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if exists, err := reader.PathExists("z"); err != nil || !exists {
		t.Fatalf("exists=%t error=%v", exists, err)
	}
	var visited []string
	err = reader.WalkDir(".", func(name string, entry fs.DirEntry, err error) error {
		visited = append(visited, name)
		return err
	})
	if err != nil || !slices.Equal(visited, []string{".", "a", "z"}) {
		t.Fatalf("walk=%v error=%v", visited, err)
	}
	if !slices.Contains(recorded, "cache/a") || !slices.Contains(recorded, "cache/.") {
		t.Fatalf("read observations missing: %v", recorded)
	}
	for _, name := range []string{"../outside", filepath.Dir(directory)} {
		if _, err := reader.ReadFile(name); err == nil {
			t.Errorf("ReadFile accepted %s", name)
		}
		if file, err := reader.Open(name); err == nil {
			file.Close()
			t.Errorf("Open accepted %s", name)
		}
		if _, err := reader.ReadDir(name); err == nil {
			t.Errorf("ReadDir accepted %s", name)
		}
		if _, err := reader.PathExists(name); err == nil {
			t.Errorf("PathExists accepted %s", name)
		}
	}
}
