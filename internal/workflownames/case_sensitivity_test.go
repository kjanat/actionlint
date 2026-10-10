package workflownames

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestCaseInsensitivePathconf(t *testing.T) {
	for _, tc := range []struct {
		value int
		err   error
		want  bool
	}{
		{0, nil, true}, {1, nil, false}, {-1, nil, false}, {2, nil, false},
		{0, os.ErrPermission, false}, {0, os.ErrNotExist, false},
	} {
		got := caseInsensitivePathconf("ancestor", func(path string, key int) (int, error) {
			if path != "ancestor" || key != 11 {
				t.Fatalf("wrong capability query: %q/%d", path, key)
			}
			return tc.value, tc.err
		})
		if got != tc.want {
			t.Errorf("value=%d error=%v: got %t, want %t", tc.value, tc.err, got, tc.want)
		}
	}
}

func TestCaseInsensitiveDirectoryEvidence(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Probe", "probe", "123"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(root, "Probe"))
	if err != nil {
		t.Fatal(err)
	}
	entry := func(name string) os.DirEntry {
		i, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return fs.FileInfoToDirEntry(i)
	}
	for _, tc := range []struct {
		name             string
		entries          []os.DirEntry
		readErr, statErr error
		want             bool
	}{
		{"case insensitive", []os.DirEntry{entry("Probe")}, nil, nil, true},
		{"case sensitive", []os.DirEntry{entry("Probe")}, nil, os.ErrNotExist, false},
		{"distinct entries", []os.DirEntry{entry("Probe"), entry("probe")}, nil, nil, false},
		{"empty directory", nil, nil, nil, false},
		{"uncased names", []os.DirEntry{entry("123")}, nil, nil, false},
		{"unreadable directory", nil, os.ErrPermission, nil, false},
		{"unreadable alias", []os.DirEntry{entry("Probe")}, nil, os.ErrPermission, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := caseInsensitiveDirectoryEntries(root, func(path string) (os.FileInfo, error) {
				if filepath.Dir(path) != root {
					t.Fatalf("inspected parent filesystem: %s", path)
				}
				if filepath.Base(path) == "probe" && tc.statErr != nil {
					return nil, tc.statErr
				}
				return info, nil
			}, func(path string) ([]os.DirEntry, error) {
				if path != root {
					t.Fatalf("inspected parent filesystem: %s", path)
				}
				return tc.entries, tc.readErr
			})
			if got != tc.want {
				t.Fatalf("got %t, want %t", got, tc.want)
			}
		})
	}
}

func TestCaseInsensitiveDirectoryIgnoresSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "Alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if caseInsensitiveDirectoryEntries(root, func(string) (os.FileInfo, error) {
		t.Fatal("used symlink evidence from another filesystem")
		return nil, os.ErrNotExist
	}, os.ReadDir) {
		t.Fatal("symlink established case-insensitive lookup")
	}
}
