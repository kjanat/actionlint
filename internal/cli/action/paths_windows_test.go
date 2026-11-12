package githubaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsForwardSlashActionPaths(t *testing.T) {
	workspace := filepath.Join(resolved(t, t.TempDir()), "workspace with spaces #é")
	if err := os.MkdirAll(filepath.Join(workspace, "nested dir"), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "forward", true: "mixed"}[mixed], func(t *testing.T) {
			path := func(value string) string {
				forward := filepath.ToSlash(value)
				if mixed {
					return strings.Replace(forward, "/", `\`, 1)
				}
				return forward
			}
			directory := filepath.Join(workspace, "nested dir")
			got, err := workingDirectory(path(workspace), path(directory))
			if err != nil || filepath.Clean(got) != directory {
				t.Fatalf("working directory: %q, %v", got, err)
			}
			got, err = resolvePath(path(directory))
			if err != nil || filepath.Clean(got) != directory {
				t.Fatalf("resolved path: %q, %v", got, err)
			}
			output := filepath.Join(workspace, "reports", "result.json")
			relative, err := resolveOutputFile(root, path(workspace), path(output))
			if err != nil || relative != filepath.Join("reports", "result.json") {
				t.Fatalf("report destination: %q, %v", relative, err)
			}
			written, err := writeResultFile(root, path(relative), "[]\n")
			if err != nil || written != "reports/result.json" {
				t.Fatalf("written report: %q, %v", written, err)
			}
			if _, err := workspaceRel(path(workspace), path(filepath.Join(workspace, "..", "outside.json")), "output-file"); err == nil {
				t.Fatal("forward-slash path escaped workspace protection")
			}
		})
	}
}
