package workspace_test

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
)

type discoveryFilesystem struct {
	files.DiskFilesystem
	directories []string
}

func (f *discoveryFilesystem) ReadDir(path string) ([]fs.DirEntry, error) {
	f.directories = append(f.directories, path)
	return f.DiskFilesystem.ReadDir(path)
}

func TestDiscoveryUsesHostFilesystemAndGitQueries(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if output, err := exec.Command(git, "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	for _, name := range []string{".github/workflows/build.yml", ".github/workflows/ignored.yml", "actions/nested/action.yaml", "ignored/action.yml", "other.yaml"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("name: fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n.github/workflows/ignored.yml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, noIgnore := range []bool{false, true} {
		t.Run(map[bool]string{false: "ignored files excluded", true: "ignored files included"}[noIgnore], func(t *testing.T) {
			filesystem := &discoveryFilesystem{}
			queries := 0
			query := func(ctx context.Context, root string, args ...string) (gitcommand.Output, error) {
				queries++
				return gitcommand.Query(ctx, git, root, nil, args...)
			}
			result, err := workspace.FindFilesWithFilesystem(t.Context(), []string{root}, noIgnore, filesystem, query)
			if err != nil {
				t.Fatal(err)
			}
			var actual []string
			for _, file := range result.Files {
				relative, err := filepath.Rel(root, file.Path)
				if err != nil {
					t.Fatal(err)
				}
				actual = append(actual, filepath.ToSlash(relative))
			}
			want := []string{".github/workflows/build.yml", "actions/nested/action.yaml"}
			if noIgnore {
				want = append(want, ".github/workflows/ignored.yml", "ignored/action.yml")
			}
			slices.Sort(actual)
			slices.Sort(want)
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("selected files: got %q, want %q", actual, want)
			}
			if len(filesystem.directories) == 0 {
				t.Fatal("discovery did not list directories through the host filesystem")
			}
			if !noIgnore && queries == 0 {
				t.Fatal("discovery did not request Git metadata from the host")
			}
			if noIgnore && queries != 0 {
				t.Fatalf("--no-ignore issued %d Git queries", queries)
			}
		})
	}
}
