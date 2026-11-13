package correctness

import (
	"fmt"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	fixturedecl "actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestCompositeConditionalModeCheckoutReset(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		condition string
		continued bool
	}{
		{"github.event.repository.name", false},
		{"github.event.repository.name", true},
		{"true", false},
		{"false", false},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			t.Parallel()
			parent := NewExecutableChecker(ExecutableContext{})
			parent.executableState = newExecutableState(true, false)
			parent.changed["source/bad.sh"] = true
			content := fmt.Appendf(nil, "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/fixture\n        if: %q\n        continue-on-error: %t\n", tc.condition, tc.continued)
			parsed := parsing.Workflow(content)
			if len(parsed.SyntaxErrors) != 0 {
				t.Fatal(parsed.SyntaxErrors)
			}
			_, _, snapshots := fixturedecl.NewCapturedSnapshots(t, capturefixture.CurrentDir(t))
			snapshot, err := snapshots.Document("workflow.yml", content, parsed)
			if err != nil {
				t.Fatal(err)
			}
			workflow, _ := snapshot.WorkflowView()
			job, _ := workflow.Job("test")
			facts, err := execution.CallFacts(slices.Collect(job.Steps())[0], execution.Paths{}.Platform)
			if err != nil {
				t.Fatal(err)
			}
			builder, err := invocation.NewBuilder(invocation.EntryWorkflow, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "fixture", Node: "call"}, facts)
			if err != nil {
				t.Fatal(err)
			}
			context, admitted := ticket.Context()
			if !admitted {
				t.Fatal("fixture call was not admitted")
			}
			child := parent.ForkComposite(context, "", execution.Paths{})
			if tc.condition != "false" {
				// A successful child checkout replaces the old mode observations before
				// a later chmod records a new mutation in that branch.
				child.afterKnownCheckout("source", false)
				child.changed["source/scripts/bad.sh"] = true
			}
			parent.JoinComposite(context, child)
			if parent.changed["source/bad.sh"] != (tc.condition != "true") || parent.changed["source/scripts/bad.sh"] != (tc.condition != "false") {
				t.Fatalf("checkout branch modes not preserved: %v", parent.changed)
			}
		})
	}
}
