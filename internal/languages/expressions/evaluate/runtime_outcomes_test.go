package evaluate

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func TestRuntimeOutcomesThroughParentExpressions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		state values.EvaluationState
		value any
	}{
		{"fromJSON(null)", values.EvaluationInvalid, nil},
		{"!fromJSON(null)", values.EvaluationInvalid, nil},
		{"fromJSON(null) || true", values.EvaluationInvalid, nil},
		{"false || fromJSON(null)", values.EvaluationInvalid, nil},
		{"true && fromJSON(null)", values.EvaluationInvalid, nil},
		{"true || fromJSON(null)", values.EvaluationKnown, true},
		{"false && fromJSON(null)", values.EvaluationKnown, false},
		{"fromJSON(null) == false", values.EvaluationInvalid, nil},
		{"false == fromJSON(null)", values.EvaluationInvalid, nil},
		{"fromJSON(null).value", values.EvaluationInvalid, nil},
		{"fromJSON(null)[0]", values.EvaluationInvalid, nil},
		{"fromJSON('[1]')[fromJSON(null)]", values.EvaluationInvalid, nil},
		{"format('{0}', fromJSON(null))", values.EvaluationInvalid, nil},
		{"format('{0}', true || fromJSON(null))", values.EvaluationKnown, "true"},
		{"case(false, fromJSON(null), 'fallback')", values.EvaluationKnown, "fallback"},
		{"case(true, fromJSON(null), 'fallback')", values.EvaluationInvalid, nil},
		{"contains(fromJSON('[]'), fromJSON(null))", values.EvaluationKnown, false},
		{"missing || fromJSON(null)", values.EvaluationUnknown, nil},
		{"format('{0}', missing)", values.EvaluationUnknown, nil},
		{"format()", values.EvaluationNotAttempted, nil},
		{"!format()", values.EvaluationNotAttempted, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node := syntax.ParseAssignedExpression("${{ " + tc.input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			got := Evaluate(node, nil)
			if got.State != tc.state || !reflect.DeepEqual(got.Value, tc.value) {
				t.Fatalf("result = %+v, want state %v and value %v", got, tc.state, tc.value)
			}
		})
	}
}

func TestRuntimeFailureStopsLaterArguments(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"fromJSON(null) == later",
		"fromJSON(null)[later]",
		"format('{0}{1}', fromJSON(null), later)",
		"contains(fromJSON(null), later)",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			node := syntax.ParseAssignedExpression("${{ " + input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			got := Evaluate(node, func(name string) (any, bool) {
				t.Errorf("evaluated variable %q after a runtime failure", name)
				return "value", true
			})
			if got.State != values.EvaluationInvalid {
				t.Fatalf("state = %v, want runtime failure", got.State)
			}
		})
	}
}
