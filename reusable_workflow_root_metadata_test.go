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

func TestReusableWorkflowDuplicateRootCacheModeMetadataOrder(t *testing.T) {
	for _, tc := range []struct {
		first, second, leaf string
		wantMismatch        bool
	}{
		{"read", "write", "read", false},
		{"write", "read", "read", true},
		{"read", "write", "write", true},
	} {
		t.Run(fmt.Sprintf("%s/%s/leaf=%s", tc.first, tc.second, tc.leaf), func(t *testing.T) {
			root := t.TempDir()
			project := &Project{root: root}
			caller := []byte("on: push\ncache-mode: read\njobs:\n  call:\n    uses: $/callee.yaml\n")
			callee := fmt.Appendf(nil, "on: workflow_call\ncache-mode: %s\ncache-mode: %s\njobs:\n  nested:\n    uses: $/leaf.yaml\n", tc.first, tc.second)
			leaf := fmt.Appendf(nil, "on: workflow_call\ncache-mode: %s\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n", tc.leaf)
			for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee, "leaf.yaml": leaf} {
				if err := os.WriteFile(filepath.Join(root, name), source, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workflow, diagnostics := Parse(callee)
			if len(diagnostics) != 1 || diagnostics[0].Kind != "syntax-check" || diagnostics[0].Line != 3 || !strings.Contains(diagnostics[0].Message, `key "cache-mode" is duplicated`) {
				t.Fatalf("expected the duplicate root declaration diagnostic, got %v", diagnostics)
			}
			event, ok := workflow.FindWorkflowCallEvent()
			if !ok {
				t.Fatal("workflow_call event missing")
			}
			linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			var previousMetadata *ReusableWorkflowMetadata
			var previousDiagnostics []*Error
			for _, fromAST := range []bool{false, true} {
				cache := NewLocalReusableWorkflowCache(project, root, nil)
				if fromAST {
					cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
				}
				metadata, err := cache.FindMetadata("./callee.yaml")
				if err != nil {
					t.Fatalf("AST=%v: duplicate root declaration prevented metadata extraction: %v", fromAST, err)
				}
				if fromAST {
					if diff := cmp.Diff(previousMetadata.JobCacheAccess, metadata.JobCacheAccess); diff != "" {
						t.Fatalf("root cache metadata differs by population order: %s", diff)
					}
				}
				previousMetadata = metadata
				for pass := range 2 {
					errs, err := linter.check("caller.yaml", caller, project, nil, nil, cache)
					if err != nil {
						t.Fatal(err)
					}
					want := 0
					if tc.wantMismatch {
						want = 1
						if len(errs) == 1 && (errs[0].Kind != "workflow-call" || errs[0].Line != 5 || !strings.Contains(errs[0].Message, `requests cache-mode "write" but the calling job allows "read"`)) {
							t.Fatalf("unexpected caller diagnostic: %v", errs)
						}
					}
					if len(errs) != want {
						t.Fatalf("AST=%v pass=%d: wanted %d caller diagnostics, got %v", fromAST, pass, want, errs)
					}
					if fromAST {
						if diff := cmp.Diff(previousDiagnostics, errs, cmp.AllowUnexported(Error{})); diff != "" {
							t.Fatalf("caller diagnostics differ by cache population order: %s", diff)
						}
					}
					previousDiagnostics = errs
				}
				errs, err := linter.check("callee.yaml", callee, project, nil, nil, cache)
				if err != nil {
					t.Fatal(err)
				}
				duplicates := 0
				for _, diagnostic := range errs {
					if diagnostic.Kind == "syntax-check" && strings.Contains(diagnostic.Message, `key "cache-mode" is duplicated`) {
						duplicates++
					}
				}
				if duplicates != 1 {
					t.Fatalf("AST=%v: expected one duplicate syntax diagnostic, got %v", fromAST, errs)
				}
			}
		})
	}
}
