package checking_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
)

func TestFileRequestsUseProcessDirectoryWithoutMutatingPaths(t *testing.T) {
	for _, kind := range []string{"relative", "absolute"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			processDirectory := t.TempDir()
			t.Chdir(processDirectory)
			target := filepath.Join(root, "workflow.yml")
			if err := os.WriteFile(target, []byte(workflow), 0o600); err != nil {
				t.Fatal(err)
			}
			path := target
			if kind == "relative" {
				var err error
				path, err = filepath.Rel(processDirectory, target)
				if err != nil {
					t.Fatal(err)
				}
			}
			paths := []string{path}
			result, err := checking.Check(t.Context(), checking.Options{
				WorkingDir: root, ReadFile: func(actual string) ([]byte, error) {
					if actual != path {
						return nil, fmt.Errorf("read %q; want %q", actual, path)
					}
					return []byte(workflow), nil
				}, SkipProjectConfig: true,
			}, inputmodel.Request{Kind: inputmodel.Files, Paths: paths})
			if err != nil || result == nil || result.FileCount != 1 || len(result.Diagnostics) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if paths[0] != path {
				t.Fatalf("mutated caller paths: %v", paths)
			}
		})
	}
}

func TestStdinRequestUsesReaderWithoutReadingAFile(t *testing.T) {
	t.Chdir(t.TempDir())
	result, err := checking.Check(t.Context(), checking.Options{
		WorkingDir: t.TempDir(), SkipProjectConfig: true,
		ReadFile: func(path string) ([]byte, error) {
			t.Fatalf("stdin requested a filesystem read: %s", path)
			return nil, nil
		},
	}, inputmodel.Request{Kind: inputmodel.Stdin, Name: "<stdin>", Reader: strings.NewReader(workflow)})
	if err != nil || result == nil || result.FileCount != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRepositoryRequestsUseProcessDirectoryOrDefaultToWorkingDirectory(t *testing.T) {
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
			processDirectory := t.TempDir()
			t.Chdir(processDirectory)
			directory, workingDir := repository, root
			switch kind {
			case "relative":
				var err error
				directory, err = filepath.Rel(processDirectory, repository)
				if err != nil {
					t.Fatal(err)
				}
			case "empty":
				directory, workingDir = "", repository
			}
			result, err := checking.Check(t.Context(), checking.Options{
				WorkingDir: workingDir, SkipProjectConfig: true,
			}, inputmodel.Request{Kind: inputmodel.Repository, Directory: directory})
			if err != nil || result == nil || result.FileCount != 1 || len(result.Diagnostics) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if !slices.Contains(result.Inputs, path) {
				t.Fatalf("missing repository input %s: %v", path, result.Inputs)
			}
		})
	}
}
