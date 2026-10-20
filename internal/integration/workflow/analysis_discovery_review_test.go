package workflow_test

import (
	"os"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestRelativeProjectKeepsInMemoryWorkflowMetadata(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	project, err := workspace.NewProject(".")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(`on: workflow_call
jobs:
  recurse:
    uses: ./.github/workflows/memory.yml
`)
	for _, cwd := range []string{".", root} {
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: cwd, ReadFile: func(path string) ([]byte, error) {
			t.Errorf("in-memory workflow metadata was read from disk: %s", path)
			return nil, os.ErrNotExist
		}}, []workspace.Source{{Path: ".github/workflows/memory.yml", Content: content, Project: project}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("in-memory metadata lost for working directory %q: %+v, %v", cwd, result, err)
		}
	}
	if project.RootDir() != "." {
		t.Fatalf("caller-owned project was mutated: %q", project.RootDir())
	}
}
