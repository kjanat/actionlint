package actionlint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCacheModeLiteralExpressionMetadata(t *testing.T) {
	for _, kind := range []CacheModeKind{CacheModeRead, CacheModeWrite, CacheModeWriteOnly, CacheModeNone} {
		for _, aliased := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/alias=%v", kind, aliased), func(t *testing.T) {
				expression := "${{ '" + kind.String() + "' }}"
				workflowMode, jobMode := expression, expression
				if aliased {
					workflowMode, jobMode = "&mode "+expression, "*mode"
				}
				source := "on: workflow_call\ncache-mode: " + workflowMode + `
jobs:
  inherited:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
  explicit:
    cache-mode: ` + jobMode + `
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
  call:
    cache-mode: ` + jobMode + `
    uses: $/leaf.yaml
`
				workflow, diagnostics := Parse([]byte(source))
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, "callee.yaml"), []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				project := &Project{root: root}
				fileCache := NewLocalReusableWorkflowCache(project, root, nil)
				fromFile, err := fileCache.FindMetadata("./callee.yaml")
				if err != nil {
					t.Fatal(err)
				}
				event, ok := workflow.FindWorkflowCallEvent()
				if !ok {
					t.Fatal("missing workflow_call event")
				}
				astCache := NewLocalReusableWorkflowCache(project, root, nil)
				astCache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
				fromAST, _, ok := astCache.readCache("./callee.yaml")
				if !ok || fromAST == nil || fromFile == nil {
					t.Fatal("missing reusable workflow metadata")
				}
				if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
					t.Fatal(diff)
				}
				for id, line := range map[string]int{"inherited": 2, "explicit": 9, "call": 14} {
					mode := fromFile.JobCacheAccess[id].Mode
					want := Pos{Line: line, Col: 17}
					if line == 2 || aliased {
						want = Pos{Line: 2, Col: 13}
					}
					if mode == nil || mode.Kind != kind || *mode.Pos != want {
						t.Fatalf("%s: expected %s at %v, got %#v", id, kind, want, mode)
					}
				}
			})
		}
	}
}

func TestWorkflowCallCacheModeLiteralExpressions(t *testing.T) {
	for _, kind := range []CacheModeKind{CacheModeRead, CacheModeWrite, CacheModeWriteOnly, CacheModeNone} {
		for _, fromAST := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/AST=%v", kind, fromAST), func(t *testing.T) {
				caller := `on: push
cache-mode: ${{ 'none' }}
jobs:
  call:
    cache-mode: ${{ '` + kind.String() + `' }}
    uses: $/callee.yaml
`
				callee := `on: workflow_call
cache-mode: ${{ 'write' }}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				diagnostics := checkCacheModeCall(t, caller, map[string]string{"callee.yaml": callee}, fromAST)
				if kind == CacheModeWrite {
					if len(diagnostics) != 0 {
						t.Fatal(diagnostics)
					}
					return
				}
				if len(diagnostics) != 1 || diagnostics[0].Kind != "workflow-call" || diagnostics[0].Line != 6 || !strings.Contains(diagnostics[0].Message, `requests cache-mode "write" but the calling job allows "`+kind.String()+`"`) {
					t.Fatalf("folded cache-mode did not retain its ceiling: %v", diagnostics)
				}
			})
		}
	}
}
