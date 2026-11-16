package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestEventProjectionIsNotMutated(t *testing.T) {
	t.Parallel()
	array := types.NewArrayType(types.NewStrictObjectType(map[string]types.Type{"message": types.StringType()}).Type())
	event := types.NewStrictObjectType(map[string]types.Type{"commits": array.Type()})
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.SetContextAvailability([]string{"github"})
	contexts.SetEventPayloadType(checker, event)
	node, err := syntax.NewParser().ParseExpression(syntax.NewLexer("github.event.commits.*.message}}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, errs := checker.Check(evaluate.EvaluateSubexpressions(node)); len(errs) != 0 {
		t.Fatal(errs)
	}
	if array.Deref() {
		t.Fatal("filter mutated shared array type")
	}
	contexts.SetDispatchInputTypes(checker, types.NewStrictObjectType(map[string]types.Type{"flag": types.BoolType()}))
	if _, ok := event.Property("inputs"); ok {
		t.Fatal("dispatch update mutated shared event")
	}
}

func TestDispatchInputContextTypesAgainstRunner(t *testing.T) {
	t.Parallel()
	declared := types.NewStrictObjectType(map[string]types.Type{"flag": types.BoolType(), "count": types.NumberType()})
	// The supplied context follows the dispatch representation. Runny checks
	// expression evaluation against it, not GitHub's construction of the payload.
	variables := map[string]any{
		"inputs": map[string]any{"flag": true, "count": float64(42)},
		"github": map[string]any{"event": map[string]any{"inputs": map[string]any{"flag": "true", "count": "42"}}},
	}
	for _, tc := range []struct {
		input string
		kind  types.TypeKind
		want  any
	}{
		{"inputs.flag", types.KindBool, true},
		{"inputs.count", types.KindNumber, float64(42)},
		{"github.event.inputs.flag", types.KindString, "true"},
		{"github.event.inputs.count", types.KindString, "42"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			checker := NewChecker(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"), nil)
			contexts.SetDispatchInputTypes(checker, declared)
			node := syntax.ParseAssignedExpression("${{ " + tc.input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			result := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
			if !result.Valid || result.Type.Kind() != tc.kind {
				t.Fatalf("result=%+v, want type %v", result, tc.kind)
			}
			if value, known := evaluate.ConditionConstantValue(node); known {
				t.Fatalf("declared type invented runtime value %v", value)
			}
			value, known := evaluate.ExpressionValue(node, func(name string) (any, bool) { value, found := variables[name]; return value, found })
			if !known || value != tc.want {
				t.Fatalf("value=%v known=%t, want %v", value, known, tc.want)
			}
			expressions.CheckValue(t, tc.input, variables, tc.want)
		})
	}
}
