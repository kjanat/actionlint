package lsp

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
)

func TestEditorMemorySourcesReadClosedDependencies(t *testing.T) {
	t.Parallel()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(directory, "does-not-exist")
	workflow := filepath.Join(root, ".github", "workflows", "test.yml")
	metadata := filepath.Join(root, "local", "action.yml")
	var reads []string
	result, err := AnalyzeEditorDocuments(t.Context(), checking.Options{WorkingDir: root, ReadFile: func(path string) ([]byte, error) {
		reads = append(reads, path)
		if path == metadata {
			return []byte("name: Local\ndescription: Fixture\ninputs:\n  valid:\n    description: Input\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo hi\n"), nil
		}
		return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrNotExist}
	}}, map[string]Document{workflow: {URI: FileURI(workflow), Version: 1, Text: "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n        with:\n          invalid: value\npermissions: {}\n"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic.Message, "invalid") && strings.Contains(diagnostic.Message, "input") {
			found = true
		}
	}
	if !found {
		t.Fatalf("closed action was not validated: diagnostics=%+v reads=%v", result.Diagnostics, reads)
	}
	if _, captured := result.Sources[metadata]; !captured {
		t.Fatal("closed action source was not admitted")
	}
}
