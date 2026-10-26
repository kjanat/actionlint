package execution

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestCompositeConditionalModeCheckoutReset(t *testing.T) {
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
			parent := NewExecutableChecker(ExecutableContext{})
			parent.executableState = newExecutableState(true, false)
			parent.changed["source/bad.sh"] = true
			call := &workflowlang.Step{Exec: &workflowlang.ExecAction{}, If: &workflowlang.String{Value: tc.condition}, ContinueOnError: &workflowlang.Bool{Value: tc.continued}}
			context, err := testCallContext(t, call, Paths{}.Platform)
			if err != nil {
				t.Fatal(err)
			}
			child := parent.ForkComposite(context, "", Paths{})
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
