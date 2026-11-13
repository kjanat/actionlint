package evaluate

import (
	"reflect"
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func TestRuntimeOutcomesThroughParentExpressions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		state exprvalues.EvaluationState
		value any
	}{
		{"fromJSON(null)", exprvalues.EvaluationInvalid, nil},
		{"!fromJSON(null)", exprvalues.EvaluationInvalid, nil},
		{"fromJSON(null) || true", exprvalues.EvaluationInvalid, nil},
		{"false || fromJSON(null)", exprvalues.EvaluationInvalid, nil},
		{"true && fromJSON(null)", exprvalues.EvaluationInvalid, nil},
		{"true || fromJSON(null)", exprvalues.EvaluationKnown, true},
		{"false && fromJSON(null)", exprvalues.EvaluationKnown, false},
		{"fromJSON(null) == false", exprvalues.EvaluationInvalid, nil},
		{"false == fromJSON(null)", exprvalues.EvaluationInvalid, nil},
		{"fromJSON(null).value", exprvalues.EvaluationInvalid, nil},
		{"fromJSON(null)[0]", exprvalues.EvaluationInvalid, nil},
		{"fromJSON('[1]')[fromJSON(null)]", exprvalues.EvaluationInvalid, nil},
		{"format('{0}', fromJSON(null))", exprvalues.EvaluationInvalid, nil},
		{"format('{0}', true || fromJSON(null))", exprvalues.EvaluationKnown, "true"},
		{"case(false, fromJSON(null), 'fallback')", exprvalues.EvaluationKnown, "fallback"},
		{"case(true, fromJSON(null), 'fallback')", exprvalues.EvaluationInvalid, nil},
		{"contains(fromJSON('[]'), fromJSON(null))", exprvalues.EvaluationKnown, false},
		{"missing || fromJSON(null)", exprvalues.EvaluationUnknown, nil},
		{"format('{0}', missing)", exprvalues.EvaluationUnknown, nil},
		{"format()", exprvalues.EvaluationNotAttempted, nil},
		{"!format()", exprvalues.EvaluationNotAttempted, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node := exprsyntax.ParseAssignedExpression("${{ " + tc.input + " }}")
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
			node := exprsyntax.ParseAssignedExpression("${{ " + input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			got := Evaluate(node, func(name string) (any, bool) {
				t.Errorf("evaluated variable %q after a runtime failure", name)
				return "value", true
			})
			if got.State != exprvalues.EvaluationInvalid {
				t.Fatalf("state = %v, want runtime failure", got.State)
			}
		})
	}
}
