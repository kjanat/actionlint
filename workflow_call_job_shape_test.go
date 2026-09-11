package actionlint

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestWorkflowCallMixedJobMetadataOrder(t *testing.T) {
	for _, field := range []struct{ key, value string }{
		{"runs-on", "ubuntu-latest"}, {"steps", "[{run: echo ok}]"},
		{"environment", "production"}, {"outputs", "{result: value}"},
		{"env", "{FLAG: value}"}, {"defaults", "{run: {shell: bash}}"},
		{"timeout-minutes", "10"}, {"continue-on-error", "false"}, {"container", "ubuntu:latest"},
	} {
		for _, leaf := range []struct{ name, source string }{
			{"missing", ""}, {"malformed", "on: ["},
			{"cache mismatch", "on: workflow_call\ncache-mode: write\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"},
		} {
			t.Run(field.key+"/"+leaf.name, func(t *testing.T) {
				root := t.TempDir()
				caller := []byte("on: push\ncache-mode: read\njobs:\n  call:\n    uses: $/callee.yaml\n")
				callee := []byte("on: workflow_call\njobs:\n  mixed:\n    uses: $/leaf.yaml\n    " + field.key + ": " + field.value + "\n")
				for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee} {
					if err := os.WriteFile(filepath.Join(root, name), source, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if leaf.source != "" {
					if err := os.WriteFile(filepath.Join(root, "leaf.yaml"), []byte(leaf.source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				project := &Project{root: root}
				linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
				if err != nil {
					t.Fatal(err)
				}
				check := func(diagnostics []*Error, includeCallee bool) {
					t.Helper()
					if !includeCallee {
						if len(diagnostics) != 0 {
							t.Fatalf("invalid job created a caller diagnostic: %v", diagnostics)
						}
						return
					}
					if len(diagnostics) != 1 || diagnostics[0].Kind != "syntax-check" || diagnostics[0].Line != 5 || filepath.Base(diagnostics[0].Filepath) != "callee.yaml" || !strings.Contains(diagnostics[0].Message, fmt.Sprintf("%q is not available", field.key)) {
						t.Fatalf("expected only the mixed-job syntax error: %v", diagnostics)
					}
				}
				for _, order := range [][]string{{"caller.yaml", "callee.yaml"}, {"callee.yaml", "caller.yaml"}} {
					cache := NewLocalReusableWorkflowCache(project, root, nil)
					for range 2 {
						for _, name := range order {
							source := caller
							if name == "callee.yaml" {
								source = callee
							}
							diagnostics, err := linter.check(name, source, project, nil, nil, cache)
							if err != nil {
								t.Fatal(err)
							}
							check(diagnostics, name == "callee.yaml")
						}
					}
					if _, _, found := cache.readCache("./leaf.yaml"); found {
						t.Fatal("invalid job populated leaf cache")
					}
					diagnostics, err := linter.LintFiles([]string{filepath.Join(root, order[0]), filepath.Join(root, order[1])}, project)
					if err != nil {
						t.Fatal(err)
					}
					check(diagnostics, true)
				}
				workflow, _ := Parse(callee)
				event, _ := workflow.FindWorkflowCallEvent()
				cache := NewLocalReusableWorkflowCache(project, root, nil)
				cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
				fromAST, _, _ := cache.readCache("./callee.yaml")
				fromFile, err := parseReusableWorkflowMetadata(callee)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}
