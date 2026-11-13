package conditions

import (
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

// https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#status-check-functions
// Check the analyzer's reachability decisions, not actual runner execution.
func TestGitHubReferenceStatusChecks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input                       string
		afterFailure, whenCancelled bool
	}{
		{"true", false, false},
		{"steps.demo.conclusion == 'failure'", false, false},
		{"success()", false, true},
		{"always()", true, true},
		{"cancelled()", true, true},
		{"!cancelled()", true, false},
		{"failure()", true, true},
		{"failure() && steps.demo.conclusion == 'failure'", true, true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node := exprsyntax.ParseAssignedExpression("${{ " + tc.input + " }}")
			if node == nil {
				t.Fatal("invalid reference expression")
			}
			if got := CanRunAfterFailure(value.Condition(tc.input, value.Origin{})); got != tc.afterFailure {
				t.Errorf("can run after failure=%t, want %t", got, tc.afterFailure)
			}
			// Cancellation and step/action status are separate inputs, including
			// cancellation during composite cleanup after earlier steps succeeded.
			if got := CanRunDuringCancellation(node); got != tc.whenCancelled {
				t.Errorf("can run during cancellation=%t, want %t", got, tc.whenCancelled)
			}
		})
	}
	condition := value.Condition("failure()", value.Origin{})
	if enabled, known := JobCondition(condition, false); enabled || !known {
		t.Fatalf("job without ancestors: enabled=%t known=%t", enabled, known)
	}
	if _, known := JobCondition(condition, true); known {
		t.Fatal("ancestor outcome was assumed without execution evidence")
	}
}
