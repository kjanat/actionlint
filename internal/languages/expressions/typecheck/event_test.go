package typecheck

import (
	"testing"

	runnerreference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestEventProjectionIsNotMutated(t *testing.T) {
	t.Parallel()
	array := exprtypes.NewArrayType(exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"message": exprtypes.StringType()}).Type())
	event := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"commits": array.Type()})
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.SetContextAvailability([]string{"github"})
	contextspec.SetEventPayloadType(checker, event)
	node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer("github.event.commits.*.message}}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, errs := checker.Check(expreval.EvaluateSubexpressions(node)); len(errs) != 0 {
		t.Fatal(errs)
	}
	if array.Deref() {
		t.Fatal("filter mutated shared array type")
	}
	contextspec.SetDispatchInputTypes(checker, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"flag": exprtypes.BoolType()}))
	if _, ok := event.Property("inputs"); ok {
		t.Fatal("dispatch update mutated shared event")
	}
}

func TestDispatchInputContextTypesAgainstRunner(t *testing.T) {
	t.Parallel()
	declared := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"flag": exprtypes.BoolType(), "count": exprtypes.NumberType()})
	// The supplied context follows the dispatch representation. Runny checks
	// expression evaluation against it, not GitHub's construction of the payload.
	variables := map[string]any{
		"inputs": map[string]any{"flag": true, "count": float64(42)},
		"github": map[string]any{"event": map[string]any{"inputs": map[string]any{"flag": "true", "count": "42"}}},
	}
	for _, tc := range []struct {
		input string
		kind  exprtypes.Kind
		want  any
	}{
		{"inputs.flag", exprtypes.KindBool, true},
		{"inputs.count", exprtypes.KindNumber, float64(42)},
		{"github.event.inputs.flag", exprtypes.KindString, "true"},
		{"github.event.inputs.count", exprtypes.KindString, "42"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			checker := NewChecker(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"), nil)
			contextspec.SetDispatchInputTypes(checker, declared)
			node := exprsyntax.ParseAssignedExpression("${{ " + tc.input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if !result.Valid || result.Type.Kind() != tc.kind {
				t.Fatalf("result=%+v, want type %v", result, tc.kind)
			}
			if value, known := expreval.ConditionConstantValue(node); known {
				t.Fatalf("declared type invented runtime value %v", value)
			}
			value, known := expreval.ExpressionValue(node, func(name string) (any, bool) { value, found := variables[name]; return value, found })
			if !known || value != tc.want {
				t.Fatalf("value=%v known=%t, want %v", value, known, tc.want)
			}
			runnerreference.CheckValue(t, tc.input, variables, tc.want)
		})
	}
}
