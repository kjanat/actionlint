package builtin_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/engine"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
)

func TestReusableMetadataFailureKeepsOtherEntryFindingsInBothOrders(t *testing.T) {
	for _, callerFirst := range []bool{false, true} {
		name := "callee-first"
		if callerFirst {
			name = "caller-first"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			calleePath := filepath.Join(root, "callee.yml")
			calleeBytes := []byte("on:\n  workflow_call:\n    inputs: invalid\njobs:\n  test:\n    runs-on: ubuntu-latset\n    steps: [{run: echo ok}]\n")
			if _, err := calls.ParseMetadata(calleeBytes); err == nil {
				t.Fatal("fixture must fail reusable metadata parsing")
			}
			registry := newCapturedSnapshots(t, root)
			callee, err := registry.Document(calleePath, calleeBytes, documentparse.Workflow(calleePath, calleeBytes))
			if err != nil {
				t.Fatal(err)
			}
			workflow, ok := callee.Workflow()
			if !ok {
				t.Fatal("callee has no recovered workflow")
			}
			if _, reusable := workflow.FindWorkflowCallEvent(); !reusable {
				t.Fatal("fixture did not recover the reusable event")
			}
			callerPath := filepath.Join(root, "caller.yml")
			callerBytes := []byte("on: push\njobs: {call: {uses: ./callee.yml}}\n")
			caller, err := registry.Document(callerPath, callerBytes, documentparse.Workflow(callerPath, callerBytes))
			if err != nil {
				t.Fatal(err)
			}
			cache := calls.NewCache(root, root, nil)
			cache.SetSources(t.Context(), registry.Sources.Store, root)
			visit := func(snapshot *documentparse.SourceSnapshot) (int, int) {
				t.Helper()
				workflow, available := snapshot.Workflow()
				if !available {
					t.Fatal("workflow is unavailable")
				}
				callRule := builtin.NewRuleWorkflowCall(snapshot, cache)
				labelRule := builtin.NewRuleRunnerLabel(snapshot)
				visitor := engine.NewVisitor()
				visitor.AddPass(callRule)
				visitor.AddPass(labelRule)
				if err := visitor.Visit(workflow); err != nil {
					t.Fatalf("metadata cache failure stopped recovered entry traversal: %v", err)
				}
				return len(callRule.Findings), len(labelRule.Findings)
			}
			var callerFindings, calleeFindings int
			if callerFirst {
				callerFindings, _ = visit(caller)
				_, calleeFindings = visit(callee)
			} else {
				_, calleeFindings = visit(callee)
				callerFindings, _ = visit(caller)
			}
			if callerFindings != 1 || calleeFindings != 1 {
				t.Fatalf("metadata ordering lost findings: caller=%d callee runner-label=%d", callerFindings, calleeFindings)
			}
			if metadata, err, cached := cache.Lookup("./callee.yml"); metadata != nil || err == nil || !cached {
				t.Fatalf("caller-facing metadata failure was not cached: metadata=%+v error=%v cached=%v", metadata, err, cached)
			}
		})
	}
}
