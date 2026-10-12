package projectpath

import (
	"os"
	"path/filepath"
	"testing"
)

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
