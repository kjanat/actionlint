package execution

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
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
			call := &workflowlang.Step{If: &workflowlang.String{Value: tc.condition}, ContinueOnError: &workflowlang.Bool{Value: tc.continued}}
			child := parent.ForkComposite(call, "", Paths{})
			if tc.condition != "false" {
				// A successful child checkout replaces the old mode observations before
				// a later chmod records a new mutation in that branch.
				child.afterKnownCheckout("source", false)
				child.changed["source/scripts/bad.sh"] = true
			}
			parent.JoinComposite(call, child)
			if parent.changed["source/bad.sh"] != (tc.condition != "true") || parent.changed["source/scripts/bad.sh"] != (tc.condition != "false") {
				t.Fatalf("checkout branch modes not preserved: %v", parent.changed)
			}
		})
	}
}
