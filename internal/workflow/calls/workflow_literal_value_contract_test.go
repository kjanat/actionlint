package calls_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	"github.com/google/go-cmp/cmp"
)

func TestWorkflowLiteralCallMetadata(t *testing.T) {
	source := []byte(`on: workflow_call
jobs:
  call:
    uses: ${{ '$/leaf.yaml' }}
`)
	w, errs := workflowparser.ParseDocument(source)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	root := t.TempDir()
	cache := newCapturedWorkflowCache(t, root, root, nil)
	event, _ := w.FindWorkflowCallEvent()
	cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, w)
	fromAST, _, _ := cache.Lookup("./callee.yaml")
	fromFile, err := calls.ParseMetadata(source)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
		t.Fatal(diff)
	}
	if access := fromFile.JobCacheAccess["call"]; access.Uses != "./leaf.yaml" || access.SourceUses != "$/leaf.yaml" {
		t.Fatalf("literal call lost: %#v", access)
	}
}
