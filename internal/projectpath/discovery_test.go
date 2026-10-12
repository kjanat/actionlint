package projectpath

import (
	"os"
	"path/filepath"
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
	if HasWorkflowFiles(root) {
		t.Fatal("empty workflows directory reported workflow files")
	}
	if HasWorkflowFiles(filepath.Join(root, "inner")) {
		t.Fatal("repository without a workflows directory reported workflow files")
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "workflows", "sub", "ci.yaml"), []byte("on: push\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !HasWorkflowFiles(root) {
		t.Fatal("nested workflow file was not found")
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
