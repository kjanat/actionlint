package application_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/application"
)

func TestContentPathDetectsProject(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "testdata", "find_project"))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".github", "workflows", "test.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path   string
		wantFindings bool
	}{
		{"existing path", path, true},
		{"stdin", "<stdin>", false},
		{"missing path", filepath.Join("this-dir-doesnt-exist", "not-found.yaml"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := application.Check(t.Context(), application.Options{WorkingDir: t.TempDir()}, application.Input{Kind: application.Stdin, Name: tc.path, Reader: bytes.NewReader(content)})
			if err != nil {
				t.Fatal(err)
			}
			if (len(result.Diagnostics) > 0) != tc.wantFindings {
				t.Fatalf("path %q: diagnostics=%+v; want findings=%t", tc.path, result.Diagnostics, tc.wantFindings)
			}
		})
	}
}
