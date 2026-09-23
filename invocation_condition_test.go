package actionlint

import "testing"

func TestConditionNeverRuns(t *testing.T) {
	for _, tc := range []struct {
		expression string
		never      bool
	}{
		{"success() && failure()", true},
		{"failure() && success()", true},
		{"SUCCESS() && FAILURE()", true},
		{"success() && !success()", true},
		{"cancelled() && !cancelled()", true},
		{"!always()", true},
		{"github.event_name == 'push' && success() && failure()", true},
		{"false && github.event_name == 'push'", true},
		{"(success() && failure()) || false", true},
		{"success() || failure()", false},
		{"!success() && !failure()", false},
		{"success() && cancelled()", false},
		{"failure() && cancelled()", false},
		{"success() && github.event_name == 'push'", false},
		{"(success() && failure()) || github.event_name == 'push'", false},
		{"success('unexpected') && failure()", false},
		{"fromJSON(inputs.enabled)", false},
		{"!0 && 'false'", false},
		{"'' || null", true},
		{"always()", false},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			expression := parseAssignedExpression("${{ " + tc.expression + " }}")
			if expression == nil {
				t.Fatal("invalid test expression")
			}
			if got := conditionNeverRuns(expression); got != tc.never {
				t.Errorf("conditionNeverRuns() = %v, want %v", got, tc.never)
			}
		})
	}
	if conditionNeverRuns(nil) {
		t.Error("missing expression must remain unknown")
	}
}
