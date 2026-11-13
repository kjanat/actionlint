package runnerpaths_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/files/runnerpaths"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestShellcheckWorkingDirectorySymlinks(t *testing.T) {
	t.Parallel()
	root, outside, analyzer := t.TempDir(), t.TempDir(), t.TempDir()
	inside := filepath.Join(root, "scripts")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ name, target string }{
		{"internal", inside},
		{"external", outside},
		{"chain", filepath.Join(root, "external")},
		{"broken", filepath.Join(root, "missing")},
	} {
		if err := os.Symlink(link.target, filepath.Join(root, link.name)); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
	}
	alias := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	for _, workspace := range []string{root, alias} {
		for _, directory := range []string{"internal", "external", "chain", "broken"} {
			t.Run(filepath.Base(workspace)+"/"+directory, func(t *testing.T) {
				t.Parallel()
				paths := execution.Paths{}
				paths.Workspace, paths.Analysis = workspace, analyzer
				got := runnerpaths.ResolveDirectory(paths, execution.Directory{Kind: execution.DirectoryKnown, Path: directory}, files.New(t.Context(), nil, nil))
				if directory != "broken" {
					target := outside
					if directory == "internal" {
						target = inside
					}
					canonical, err := filepath.EvalSymlinks(target)
					if err != nil {
						t.Fatal(err)
					}
					if got.Kind != execution.DirectoryKnown || got.Path != canonical {
						t.Fatalf("available linked directory should resolve: %+v", got)
					}
				} else if got.Kind != execution.DirectoryUnknown || got.Path != analyzer {
					t.Fatalf("unavailable local directory should use analysis fallback: %+v", got)
				}
			})
		}
	}
}
