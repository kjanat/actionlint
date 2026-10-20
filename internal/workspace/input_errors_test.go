package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workspace"
)

type failingInputReader struct{}

func (failingInputReader) Read([]byte) (int, error) { return 0, errors.New("dummy read error") }
func TestStdinReadFailure(t *testing.T) {
	session, err := workspace.New(workspace.Options{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.ReadStdin(failingInputReader{}, false)
	if err == nil || err.Error() != "could not read stdin: dummy read error" {
		t.Fatalf("wrong read error: %v", err)
	}
}

func TestMissingInputPaths(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, "existing"), 0700); err != nil {
		t.Fatal(err)
	}
	session, err := workspace.New(workspace.Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join("existing", "missing.yaml"), filepath.Join("missing", "workflow.yaml"), filepath.Join(root, "missing.yaml"), filepath.Join(root, "missing", "workflow.yaml")} {
		t.Run(path, func(t *testing.T) {
			_, err := session.Files([]string{path}, nil)
			if err == nil || !strings.Contains(err.Error(), "could not read") {
				t.Fatalf("wrong missing input error: %v", err)
			}
		})
	}
}
