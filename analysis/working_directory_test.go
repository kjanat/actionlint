package analysis_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
)

func TestFilesRootsRelativeTargetsInWorkingDirectory(t *testing.T) {
	for _, kind := range []string{"relative", "absolute", "stdin"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(t.TempDir())
			target := filepath.Join(root, "workflow.yml")
			if err := os.WriteFile(target, []byte(workflow), 0o600); err != nil {
				t.Fatal(err)
			}
			path, expected := target, target
			switch kind {
			case "relative":
				path = "workflow.yml"
			case "stdin":
				path, expected = "<stdin>", "<stdin>"
			}
			paths := []string{path}
			result, err := analysis.Files(t.Context(), paths, analysis.FileOptions{
				WorkingDir: root, ReadFile: func(actual string) ([]byte, error) {
					if actual != expected {
						return nil, fmt.Errorf("read %q; want %q", actual, expected)
					}
					return []byte(workflow), nil
				}, SkipProjectConfig: true,
			})
			if err != nil || result == nil || result.FileCount != 1 || len(result.Diagnostics) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if paths[0] != path {
				t.Fatalf("mutated caller paths: %v", paths)
			}
		})
	}
}

func TestRepositoryRootsRelativeDirectoryInWorkingDirectory(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required for repository discovery")
	}
	for _, kind := range []string{"relative", "absolute", "empty"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			repository := filepath.Join(root, "project")
			path := filepath.Join(repository, ".github", "workflows", "ci.yml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), git, "init", "--quiet", repository)
			command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(root, "unused-gitconfig"))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			t.Chdir(t.TempDir())
			directory, workingDir := repository, root
			switch kind {
			case "relative":
				directory = "project"
			case "empty":
				directory, workingDir = "", repository
			}
			result, err := analysis.Repository(t.Context(), directory, analysis.FileOptions{
				WorkingDir: workingDir, SkipProjectConfig: true,
			})
			if err != nil || result == nil || result.FileCount != 1 || len(result.Diagnostics) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if !slices.Contains(result.Inputs, path) {
				t.Fatalf("missing repository input %s: %v", path, result.Inputs)
			}
		})
	}
}
