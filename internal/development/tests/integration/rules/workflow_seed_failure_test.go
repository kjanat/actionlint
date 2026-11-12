package rules_test

import (
	"path/filepath"
	"testing"

	runnerinput "actionlint.kjanat.dev/v2/internal/input/runners"

	"actionlint.kjanat.dev/v2/internal/checking/passes"
	repofacts "actionlint.kjanat.dev/v2/internal/model/repositories"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestReusableMetadataFailureKeepsOtherEntryFindingsInBothOrders(t *testing.T) {
	t.Parallel()
	for _, callerFirst := range []bool{false, true} {
		name := "callee-first"
		if callerFirst {
			name = "caller-first"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			calleePath := filepath.Join(root, "callee.yml")
			calleeBytes := []byte("on:\n  workflow_call:\n    inputs: invalid\njobs:\n  test:\n    runs-on: ubuntu-latset\n    steps: [{run: echo ok}]\n")
			if _, err := calls.ParseMetadata(calleeBytes); err == nil {
				t.Fatal("fixture must fail reusable metadata parsing")
			}
			store, registry := newCapturedSnapshots(t, root)
			callee, err := registry.Document(calleePath, calleeBytes, parsing.Workflow(calleeBytes))
			if err != nil {
				t.Fatal(err)
			}
			workflow, ok := callee.WorkflowView()
			if !ok {
				t.Fatal("callee has no recovered workflow")
			}
			reusable := false
			for event := range workflow.On() {
				if _, ok := event.WorkflowCallEvent(); ok {
					reusable = true
				}
			}
			if !reusable {
				t.Fatal("fixture did not recover the reusable event")
			}
			callerPath := filepath.Join(root, "caller.yml")
			callerBytes := []byte("on: push\njobs: {call: {uses: ./callee.yml}}\n")
			caller, err := registry.Document(callerPath, callerBytes, parsing.Workflow(callerBytes))
			if err != nil {
				t.Fatal(err)
			}
			cache := workflowinputs.NewCache(root, nil)
			cache.SetSources(t.Context(), store, root)
			visit := func(snapshot parsing.SourceSnapshot) (int, int) {
				t.Helper()
				workflow, available := snapshot.WorkflowView()
				if !available {
					t.Fatal("workflow is unavailable")
				}
				runners, err := runnerinput.Prepare(t.Context(), snapshot, nil, runnerapi.Options{}, "scope")
				if err != nil {
					t.Fatal(err)
				}
				if err := cache.Seed(snapshot); err != nil {
					t.Fatal(err)
				}
				callRule := workflowCallVisitor(t, workflow, cache, repofacts.Snapshot{})
				labelRule := productionRunnerRule(t, snapshot, runners)
				if err := passes.Visit(workflow, passes.Options{Passes: []workflowvisit.Pass{callRule, labelRule}}); err != nil {
					t.Fatalf("metadata cache failure stopped recovered entry traversal: %v", err)
				}
				return len(callRule.Errs()), len(labelRule.Errs())
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
