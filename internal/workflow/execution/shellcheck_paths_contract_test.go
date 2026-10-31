package execution_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

func TestShellcheckAbsoluteDirectoryPlatform(t *testing.T) {
	directory := t.TempDir()
	for _, platform := range []runnerlang.Platform{runnerlang.Any, runnerlang.Windows, runnerlang.MacOrLinux} {
		path, known := execution.LocalDirectoryPath(directory, platform)
		wantKnown := platform == runnerlang.Any ||
			(platform == runnerlang.Windows) == (runtime.GOOS == "windows")
		if known != wantKnown || known && path != filepath.Clean(directory) {
			t.Fatalf("platform %v: got %q, known=%v; want known=%v", platform, path, known, wantKnown)
		}
	}
}

func TestShellcheckWindowsRootedDirectory(t *testing.T) {
	for _, path := range []string{`\scripts\build`, `\\server\share`, `\\?\C:\scripts`} {
		if runtime.GOOS != "windows" {
			if got, known := execution.LocalDirectoryPath(path, runnerlang.Windows); known {
				t.Errorf("Windows path %q mapped to Unix path %q", path, got)
			}
		}
	}
	if got, known := execution.LocalDirectoryPath(`scripts\build`, runnerlang.Windows); !known || got != "scripts/build" {
		t.Errorf("relative Windows path lost: %q, known=%v", got, known)
	}
}

func TestShellcheckWorkingDirectorySymlinks(t *testing.T) {
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
				paths := execution.Paths{}
				paths.Workspace, paths.Analysis = workspace, analyzer
				got := paths.Resolve(execution.Directory{Kind: execution.DirectoryKnown, Path: directory}, fsread.New(t.Context(), nil, nil))
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
