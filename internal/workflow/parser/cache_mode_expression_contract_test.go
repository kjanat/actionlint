package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workspace"

	"github.com/google/go-cmp/cmp"
)

func TestCacheModeLiteralExpressionMetadata(t *testing.T) {
	for _, kind := range []workflowlang.CacheModeKind{workflowlang.CacheModeRead, workflowlang.CacheModeWrite, workflowlang.CacheModeWriteOnly, workflowlang.CacheModeNone} {
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
				workflow, diagnostics := workflowparser.ParseDocument([]byte(source))
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, "callee.yaml"), []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
				project := workspace.Identity(root)
				snapshots := declarations.NewCapturedSnapshots(t, root)
				fileCache := calls.NewCache(project.RootDir(), root, nil)
				fileCache.SetSources(t.Context(), snapshots.Sources.Store, root)
				fromFile, err := fileCache.FindMetadata("./callee.yaml")
				if err != nil {
					t.Fatal(err)
				}
				_, ok := workflowlang.FindWorkflowCallEvent(workflow)
				if !ok {
					t.Fatal("missing workflow_call event")
				}
				astCache := calls.NewCache(project.RootDir(), root, nil)
				astCache.SetSources(t.Context(), snapshots.Sources.Store, root)
				path := filepath.Join(root, "callee.yaml")
				snapshot, err := snapshots.Document(path, []byte(source), documentparse.Workflow(path, []byte(source)))
				if err != nil {
					t.Fatal(err)
				}
				if err := astCache.Seed(snapshot); err != nil {
					t.Fatal(err)
				}
				fromAST, _, ok := astCache.Lookup("./callee.yaml")
				if !ok || fromAST == nil || fromFile == nil {
					t.Fatal("missing reusable workflow metadata")
				}
				if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
					t.Fatal(diff)
				}
				for id, line := range map[string]int{"inherited": 2, "explicit": 9, "call": 14} {
					if recorded, declared := fromFile.JobCacheAccess[id].Mode.Kind(); !declared || recorded != kind {
						t.Fatalf("%s: metadata recorded %s (declared %t), want %s", id, recorded, declared, kind)
					}
					// Metadata keeps only the mode; the parsed declaration keeps its source.
					mode := workflowlang.EffectiveCacheMode(workflow.CacheMode, workflow.Jobs[id].CacheMode)
					want := workflowlang.Pos{Line: line, Col: 17}
					if line == 2 || aliased {
						want = workflowlang.Pos{Line: 2, Col: 13}
					}
					if mode == nil || mode.Kind != kind || *mode.Pos != want {
						t.Fatalf("%s: expected %s at %v, got %#v", id, kind, want, mode)
					}
				}
			})
		}
	}
}
