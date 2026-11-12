package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestFileInputsUseCallerDirectory(t *testing.T) {
	caller, working := t.TempDir(), t.TempDir()
	t.Chdir(caller)
	for _, name := range []string{"ci.yml", ".github/workflows/ci.yml"} {
		writeToolFixture(t, caller, name, toolWorkflow)
		writeToolFixture(t, working, name, "wrong directory")
	}
	for _, path := range []string{"ci.yml", "./ci.yml", ".github/workflows"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			var reads []string
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: working, SkipProjectConfig: true, ReadFile: func(name string) ([]byte, error) {
				reads = append(reads, name)
				return os.ReadFile(name)
			}, Rules: checking.ConfigurationRules()})
			if err != nil {
				t.Fatal(err)
			}
			selection, err := session.Files([]string{path}, nil)
			if err != nil || len(selection.Sources) != 1 {
				t.Fatalf("caller input selection: %+v, %v", selection, err)
			}
			source := selection.Sources[0]
			name := path
			if path == ".github/workflows" {
				name = filepath.Join(path, "ci.yml")
			}
			if source.InputPath != filepath.Join(caller, name) || string(source.Content) != toolWorkflow {
				t.Fatalf("caller input resolved against WorkingDir: %+v", source)
			}
			if path != ".github/workflows" && (len(reads) != 1 || reads[0] != path) {
				t.Fatalf("reader lost caller spelling: %q, want %q", reads, path)
			}
		})
	}
}
