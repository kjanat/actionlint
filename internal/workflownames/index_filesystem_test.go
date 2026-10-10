package workflownames

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type inventoryEntry struct {
	name string
	info os.FileInfo
}

func (e inventoryEntry) Name() string               { return e.name }
func (e inventoryEntry) IsDir() bool                { return e.info.IsDir() }
func (e inventoryEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e inventoryEntry) Info() (os.FileInfo, error) { return e.info, nil }

func TestWorkflowPathsFilesystemIdentity(t *testing.T) {
	seed := t.TempDir()
	info := func(name string) os.FileInfo {
		t.Helper()
		path := filepath.Join(seed, name)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		value, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	first, second := info("first-workflow"), info("second-workflow")
	directory, err := os.Stat(seed)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(seed, "virtual", ".github", "workflows")
	lower, upper := filepath.Join(dir, "build.yml"), filepath.Join(dir, "BUILD.yml")
	newLower, newUpper := filepath.Join(dir, "new.yml"), filepath.Join(dir, "NEW.yml")
	for _, tc := range []struct {
		name        string
		insensitive bool
		upperInfo   os.FileInfo
		wantAlias   bool
	}{
		{"case-sensitive distinct files", false, second, false},
		{"case-sensitive hardlink names", false, first, false},
		{"case-insensitive file alias", true, nil, true},
	} {
		for _, windows := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/unix-policy", true: "/windows-policy"}[windows], func(t *testing.T) {
				files := map[string]os.FileInfo{dir: directory, lower: first}
				entries := []os.DirEntry{inventoryEntry{"build.yml", first}}
				if tc.upperInfo != nil {
					files[upper] = tc.upperInfo
					entries = append(entries, inventoryEntry{"BUILD.yml", tc.upperInfo})
				}
				stat := func(path string) (os.FileInfo, error) {
					if value, exists := files[path]; exists {
						return value, nil
					}
					if tc.insensitive {
						for existing, value := range files {
							if strings.EqualFold(path, existing) {
								return value, nil
							}
						}
					}
					return nil, os.ErrNotExist
				}
				readDir := func(path string) ([]os.DirEntry, error) {
					if path == dir || tc.insensitive && strings.EqualFold(path, dir) {
						return entries, nil
					}
					return nil, os.ErrNotExist
				}
				same := func(left, right string) bool {
					return samePathWithCaseSensitivity(left, right, windows, stat, readDir, func(path string) bool {
						if path != dir {
							t.Fatalf("case sensitivity queried outside fixture: %q", path)
						}
						return tc.insensitive
					})
				}
				if same(lower, upper) != tc.wantAlias || same(upper, lower) != tc.wantAlias {
					t.Fatal("filesystem identity was overridden by the platform policy")
				}
				want := []string{lower, upper}
				if tc.wantAlias {
					want = []string{upper}
				}
				slices.Sort(want)
				if got := workflowPathsByIdentity(dir, []string{lower, upper}, same); !slices.Equal(got, want) {
					t.Fatalf("file overlay inventory=%v, want %v", got, want)
				}
				want = []string{newLower, newUpper}
				if tc.insensitive || windows {
					want = []string{newUpper}
				}
				slices.Sort(want)
				if got := workflowPathsByIdentity(dir, []string{newLower, newUpper}, same); !slices.Equal(got, want) {
					t.Fatalf("missing overlay inventory=%v, want %v", got, want)
				}
			})
		}
	}
}
