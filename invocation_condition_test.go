package actionlint

import "testing"

func TestStepCanRunAfterFailure(t *testing.T) {
	if stepCanRunAfterFailure(nil) {
		t.Fatal("missing condition must require success")
	}
	for _, tc := range []struct {
		condition string
		want      bool
	}{
		{"", false},
		{"   ", false},
		{"${{ '' }}", false},
		{"${{ null }}", false},
		{"failure()", true},
		{"${{ failure() }}", true},
		{"${{ 'failure()' }}", true},
		{"${{ 'always()' }}", true},
		{"${{ 'cancelled()' }}", true},
		{"${{ '!success()' }}", true},
		{"${{ 'FAILURE()' }}", true},
		{"${{ 'success() || failure()' }}", true},
		{"${{ 'success()' }}", false},
		{"${{ 'success() && always()' }}", false},
		{"${{ contains(github.event_name, 'failure()') }}", false},
		{"${{ format('failure()') }}", false},
		{"${{ 'failure(' }}", true},
		{"${{ failure( }}", true},
		{"before ${{ failure() }}", true},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			if got := stepCanRunAfterFailure(&String{Value: tc.condition}); got != tc.want {
				t.Fatalf("stepCanRunAfterFailure() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInvocationCondition(t *testing.T) {
	for _, tc := range []struct {
		condition      string
		enabled, known bool
	}{
		{"success() && failure()", false, true},
		{"${{ success() && failure() }}", false, true},
		{"true", true, true},
		{"${{ false }}", false, true},
		{"success()", true, true},
		{"success() || failure()", false, false},
		{"success() && github.event_name == 'push'", false, false},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			enabled, known := invocationCondition(&String{Value: tc.condition})
			if enabled != tc.enabled || known != tc.known {
				t.Fatalf("invocationCondition() = %v, %v; want %v, %v", enabled, known, tc.enabled, tc.known)
			}
		})
	}
}

func TestJobInvocationCondition(t *testing.T) {
	for _, tc := range []struct {
		condition      string
		dependencies   bool
		enabled, known bool
	}{
		{"failure()", false, false, true},
		{"failure() && github.event_name == 'push'", false, false, true},
		{"failure()", true, false, false},
		{"failure() && cancelled()", true, false, false},
		{"cancelled()", false, false, false},
		{"!success()", false, false, false},
		{"failure() || cancelled()", false, false, false},
		{"success()", false, true, true},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			job := &Job{If: &String{Value: tc.condition}}
			if tc.dependencies {
				job.Needs = []*String{{Value: "build"}}
			}
			enabled, known := jobInvocationCondition(job)
			if enabled != tc.enabled || known != tc.known {
				t.Fatalf("jobInvocationCondition() = %v, %v; want %v, %v", enabled, known, tc.enabled, tc.known)
			}
		})
	}
}

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
