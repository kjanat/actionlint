package calls_test

import (
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"

	"github.com/google/go-cmp/cmp"
)

func TestReusableWorkflowCacheModeMetadataParity(t *testing.T) {
	src := []byte(`on: workflow_call
cache-mode: &mode read
jobs:
  base: &job
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
  alias: *job
  save:
    cache-mode: write-only
    uses: $/next.yaml
  none:
    cache-mode: none
    uses: ./next.yaml
  invalid:
    cache-mode: null
    uses: ./next.yaml
  scalar-alias:
    cache-mode: *mode
    uses: ./next.yaml
`)
	fromFile, err := calls.ParseMetadata(src)
	if err != nil {
		t.Fatal(err)
	}
	w, errs := workflowparser.ParseDocument(src)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "cache-mode") {
		t.Fatalf("expected the invalid null declaration, got %v", errs)
	}
	root := t.TempDir()
	cache := newCapturedWorkflowCache(t, root, root, nil)
	event, _ := workflowlang.FindWorkflowCallEvent(w)
	cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, w)
	fromAST, _, ok := cache.Lookup("./callee.yaml")
	if !ok {
		t.Fatal("no metadata was cached")
	}
	if diff := cmp.Diff(fromFile.JobCacheAccess, fromAST.JobCacheAccess); diff != "" {
		t.Fatal(diff)
	}
	for id, want := range map[string]workflowlang.CacheModeKind{"base": workflowlang.CacheModeRead, "alias": workflowlang.CacheModeRead, "save": workflowlang.CacheModeWriteOnly, "none": workflowlang.CacheModeNone, "invalid": workflowlang.CacheModeInvalid, "scalar-alias": workflowlang.CacheModeRead} {
		if kind, declared := fromFile.JobCacheAccess[id].Mode.Kind(); !declared || kind != want {
			t.Errorf("%s: wanted %v, got %v (declared %t)", id, want, kind, declared)
		}
	}
}
