package projectpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRepositoryRootAndWorkflowFiles(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{".git", "src/nested", "inner/.git", ".github/workflows/sub"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0750); err != nil {
			t.Fatal(err)
		}
	}
	if got := RepositoryRoot(filepath.Join(root, "src", "nested")); got != root {
		t.Fatalf("RepositoryRoot = %q, want %q", got, root)
	}
	if got := RepositoryRoot(filepath.Join(root, "inner")); got != filepath.Join(root, "inner") {
		t.Fatalf("RepositoryRoot = %q, want the nested repository", got)
	}
	if got := RepositoryRoot(filepath.Join(t.TempDir(), "missing")); got != "" {
		t.Fatalf("RepositoryRoot outside a repository = %q", got)
	}
	if found, err := HasWorkflowFiles(root); err != nil || found {
		t.Fatalf("empty workflows directory: %v, %v", found, err)
	}
	if found, err := HasWorkflowFiles(filepath.Join(root, "inner")); err != nil || found {
		t.Fatalf("repository without a workflows directory: %v, %v", found, err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "workflows", "sub", "ci.yaml"), []byte("on: push\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if found, err := HasWorkflowFiles(root); err != nil || !found {
		t.Fatalf("nested workflow file was not found: %v", err)
	}
}

func TestHasWorkflowFilesUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced")
	}
	root := t.TempDir()
	workflows := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflows, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workflows, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(workflows, 0750) })
	if found, err := HasWorkflowFiles(root); !errors.Is(err, fs.ErrPermission) || found {
		t.Fatalf("HasWorkflowFiles = %v, %v; want a permission error", found, err)
	}
}

func TestWorkflowTypo(t *testing.T) {
	for _, directory := range []string{".gihub/workflows", ".githbu/workflows", ".github/workflow", ".github/wrokflows", ".github/workflows", "unrelated/workflows"} {
		t.Run(directory, func(t *testing.T) {
			root := t.TempDir()
			for _, path := range []string{".git", "src/nested", directory} {
				if err := os.MkdirAll(filepath.Join(root, path), 0750); err != nil {
					t.Fatal(err)
				}
			}
			for _, start := range []string{root, filepath.Join(root, "src", "nested")} {
				found, expected := WorkflowTypo(start)
				if directory == ".github/workflows" || directory == "unrelated/workflows" {
					if found != "" || expected != "" {
						t.Fatalf("unexpected hint: %q -> %q", found, expected)
					}
				} else if found != filepath.Join(root, directory) || expected != filepath.Join(root, ".github", "workflows") {
					t.Fatalf("unexpected hint: %q -> %q", found, expected)
				}
			}
		})
	}
}
