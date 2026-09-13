package actionlint

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReusableWorkflowDuplicateKeyMetadataOrder(t *testing.T) {
	for _, tc := range []struct{ name, jobs string }{
		{"cache-mode", "  call:\n    uses: ./leaf.yaml\n    cache-mode: read\n    cache-mode: write\n"},
		{"uses", "  call:\n    uses: ./leaf.yaml\n    uses: ./missing.yaml\n"},
		{"permissions", "  call:\n    uses: ./leaf.yaml\n    permissions: {contents: read}\n    permissions: {contents: write}\n"},
		{"job", "  call:\n    uses: ./leaf.yaml\n  call:\n    uses: ./missing.yaml\n"},
		{"job casing", "  call:\n    uses: ./leaf.yaml\n  CALL:\n    uses: ./missing.yaml\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			project := &Project{root: root}
			caller := []byte("on: push\ncache-mode: read\npermissions: {contents: read}\njobs:\n  call:\n    uses: ./callee.yaml\n")
			callee := []byte("on: workflow_call\ncache-mode: read\njobs:\n" + tc.jobs)
			leaf := []byte("on: workflow_call\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
			for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee, "leaf.yaml": leaf} {
				if err := os.WriteFile(filepath.Join(root, name), source, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workflow, errs := Parse(callee)
			if len(errs) != 1 || !strings.Contains(errs[0].Message, "is duplicated") {
				t.Fatalf("expected one duplicate key diagnostic, got %v", errs)
			}
			event, ok := workflow.FindWorkflowCallEvent()
			if !ok {
				t.Fatal("workflow_call event missing")
			}
			astCache := NewLocalReusableWorkflowCache(project, root, nil)
			astCache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
			fromAST, _, _ := astCache.readCache("./callee.yaml")
			fromFile, err := parseReusableWorkflowMetadata(callee)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
				t.Fatal(diff)
			}
			if diff := cmp.Diff(fromAST.JobPermissions, fromFile.JobPermissions); diff != "" {
				t.Fatal(diff)
			}
			linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			check := func(errs []*Error, want int) {
				t.Helper()
				if len(errs) != want {
					t.Fatalf("wanted %d duplicate-key diagnostics, got %v", want, errs)
				}
				for _, err := range errs {
					if err.Kind != "syntax-check" || !strings.Contains(err.Message, "is duplicated") || filepath.Base(err.Filepath) != "callee.yaml" {
						t.Fatalf("unexpected diagnostic: %v", err)
					}
				}
			}
			for _, order := range [][]string{{"caller.yaml", "callee.yaml"}, {"callee.yaml", "caller.yaml"}} {
				cache := NewLocalReusableWorkflowCache(project, root, nil)
				for _, name := range order {
					source, want := caller, 0
					if name == "callee.yaml" {
						source, want = callee, 1
					}
					errs, err := linter.check(name, source, project, nil, nil, cache)
					if err != nil {
						t.Fatal(err)
					}
					check(errs, want)
				}
				if _, _, found := cache.readCache("./missing.yaml"); found {
					t.Fatal("discarded duplicate populated missing workflow cache")
				}
				errs, err := linter.LintFiles([]string{filepath.Join(root, order[0]), filepath.Join(root, order[1])}, project)
				if err != nil {
					t.Fatal(err)
				}
				check(errs, 1)
			}
		})
	}
}
