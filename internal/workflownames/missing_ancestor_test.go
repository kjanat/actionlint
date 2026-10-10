package workflownames

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMissingAncestorStopsAtUnreadablePath(t *testing.T) {
	root := t.TempDir()
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []error{os.ErrPermission, errors.New("symlink loop"), errors.New("not a directory")} {
		for _, windows := range []bool{false, true} {
			stat := func(path string) (os.FileInfo, error) {
				switch filepath.Base(path) {
				case "build.yml":
					return nil, os.ErrNotExist
				case "workflows":
					return nil, failure
				default:
					return info, nil
				}
			}
			left, right := filepath.Join(root, "checkout/workflows/build.yml"), filepath.Join(root, "alias/workflows/build.yml")
			if samePathOnFilesystem(left, right, windows, stat, os.ReadDir) {
				t.Fatalf("climbed past %v with windows=%t", failure, windows)
			}
		}
	}
}

func TestMissingAncestorRejectsFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	stat := func(path string) (os.FileInfo, error) {
		if filepath.Base(path) == "build.yml" {
			return nil, os.ErrNotExist
		}
		return info, nil
	}
	for _, windows := range []bool{false, true} {
		left, right := filepath.Join(root, "checkout/build.yml"), filepath.Join(root, "alias/build.yml")
		if samePathOnFilesystem(left, right, windows, stat, os.ReadDir) {
			t.Fatalf("file ancestor treated as a checkout directory, windows=%t", windows)
		}
	}
}

func TestMissingAncestorIdentity(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	for _, windows := range []bool{false, true} {
		for _, tc := range []struct {
			left, right string
			want        bool
		}{
			{".github/workflows", ".github/workflows", true},
			{".github/workflows/build.yml", ".github/workflows/build.yml", true},
			{".github/workflows/build.yml", ".github/workflows/other.yml", false},
			{".github/workflows/build.yml", ".other/workflows/build.yml", false},
			{".github/workflows/build.yml", ".github/workflows/BUILD.yml", windows || directoryCaseInsensitive(root, os.Stat, os.ReadDir)},
			{".github/workflows/build.yml", ".github/workflows/missing/build.yml", false},
		} {
			left, right := filepath.Join(root, tc.left), filepath.Join(alias, tc.right)
			if got := samePath(left, right, windows); got != tc.want || samePath(right, left, windows) != tc.want {
				t.Errorf("identity %q/%q windows=%t: got %t, want %t", tc.left, tc.right, windows, got, tc.want)
			}
		}
	}
	if SamePath(filepath.Join(root, "missing/build.yml"), filepath.Join(t.TempDir(), "missing/build.yml")) {
		t.Fatal("different checkout ancestors collapsed")
	}
}

func TestMissingAncestorOverlayWork(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	dir := filepath.Join(root, ".github/workflows")
	var candidates, want []string
	for i := range 1000 {
		name := fmt.Sprintf("workflow-%04d.yml", i)
		last := filepath.Join(alias, ".github/workflows", name)
		candidates = append(candidates, filepath.Join(dir, name), last)
		want = append(want, last)
	}
	calls, stats := 0, 0
	stat := func(path string) (os.FileInfo, error) { stats++; return os.Stat(path) }
	same := func(left, right string) bool {
		calls++
		return samePathOnFilesystem(left, right, false, stat, os.ReadDir)
	}
	if got := workflowPathsByIdentity(dir, candidates, same); !slices.Equal(got, want) {
		t.Fatalf("last aliased overlays not preserved: got %d, want %d", len(got), len(want))
	}
	if calls > 2*len(candidates) || stats > 12*len(candidates) {
		t.Fatalf("unbounded alias identity work: %d comparisons, %d stats", calls, stats)
	}
}

func TestMissingAncestorCaseSensitivity(t *testing.T) {
	root := t.TempDir()
	for _, insensitive := range []bool{false, true} {
		for _, suffix := range []string{".github/workflows", ".GITHUB/WORKFLOWS", ".other/workflows", ".github/workflows/extra"} {
			calls := 0
			caseInsensitive := func(path string) bool {
				calls++
				if path != root {
					t.Fatalf("queried %q instead of shared ancestor %q", path, root)
				}
				return insensitive
			}
			left, right := filepath.Join(root, ".github/workflows"), filepath.Join(root, suffix)
			got := samePathWithCaseSensitivity(left, right, false, os.Stat, os.ReadDir, caseInsensitive)
			want := suffix == ".github/workflows" || insensitive && suffix == ".GITHUB/WORKFLOWS"
			if got != want || calls > 1 {
				t.Fatalf("insensitive=%t suffix=%s: got %t, want %t (%d queries)", insensitive, suffix, got, want, calls)
			}
		}
	}
	if samePathWithCaseSensitivity(filepath.Join(root, ".github/workflows"), filepath.Join(t.TempDir(), ".GITHUB/WORKFLOWS"), false, os.Stat, os.ReadDir, func(string) bool {
		t.Fatal("queried case sensitivity for different ancestors")
		return true
	}) {
		t.Fatal("different ancestors collapsed")
	}
}
