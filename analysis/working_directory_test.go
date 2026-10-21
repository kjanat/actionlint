package analysis_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/analysis"
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
				Options: analysis.Options{WorkingDir: root, ReadFile: func(actual string) ([]byte, error) {
					if actual != expected {
						return nil, fmt.Errorf("read %q; want %q", actual, expected)
					}
					return []byte(workflow), nil
				}}, SkipProjectConfig: true,
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
