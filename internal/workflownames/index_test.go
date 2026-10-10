package workflownames

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestIndexUnnamedOverlaySpelling(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, "build.yml")
	if err := os.WriteFile(disk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "BUILD.YML")
	if runtime.GOOS != "windows" {
		overlay = disk
	}
	loads := 0
	index := &Index{Paths: []string{overlay}, Load: func(path string) (string, bool, error) {
		loads++
		if path != overlay {
			t.Errorf("loaded %q instead of original overlay %q", path, overlay)
		}
		return "", true, nil
	}}
	names, err := index.ForRoot(root)
	if err != nil || !names.Complete || len(names.Values) != 1 || !names.Values[".github/workflows/build.yml"] || loads != 1 {
		t.Fatalf("names=%+v err=%v loads=%d", names, err, loads)
	}
	if runtime.GOOS == "windows" {
		if _, err := index.ForRoot(strings.ToUpper(root)); err != nil || loads != 1 {
			t.Fatalf("case-alias root missed inventory cache: loads=%d err=%v", loads, err)
		}
	}
}

func TestWorkflowPathsPlatformIdentity(t *testing.T) {
	dir := filepath.Join("repo", ".github", "workflows")
	disk := filepath.Join(dir, "build.yml")
	overlay := filepath.Join("REPO", ".GITHUB", "WORKFLOWS", "BUILD.YML")
	other := filepath.Join(dir, "other.yaml")
	candidates := []string{disk, other, overlay,
		filepath.Join(dir, "nested", "ignored.yml"), filepath.Join(dir, "README.md")}
	for _, windows := range []bool{false, true} {
		want := []string{disk, other}
		if windows {
			want = []string{overlay, other}
		}
		slices.Sort(want)
		if got := workflowPaths(dir, candidates, windows); !slices.Equal(got, want) {
			t.Fatalf("windows=%v: paths=%v, want %v", windows, got, want)
		}
	}
	if got := PathKey(overlay) == PathKey(disk); got != (runtime.GOOS == "windows") {
		t.Fatalf("host path equality = %v on %s", got, runtime.GOOS)
	}
}

func TestWorkflowPathsLastOverlayWins(t *testing.T) {
	dir := filepath.Join("repo", ".github", "workflows")
	disk := filepath.Join(dir, "build.yml")
	first := filepath.Join(dir, "BUILD.yml")
	last := filepath.Join(dir, "Build.yml")
	if got := workflowPaths(dir, []string{disk, first, last}, true); !slices.Equal(got, []string{last}) {
		t.Fatalf("last in-memory spelling must win: %v", got)
	}
	want := []string{disk, first, last}
	slices.Sort(want)
	if got := workflowPaths(dir, []string{disk, first, last}, false); !slices.Equal(got, want) {
		t.Fatalf("case-sensitive paths must remain distinct: %v", got)
	}
}
