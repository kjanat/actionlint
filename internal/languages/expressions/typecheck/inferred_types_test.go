package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestInferredTypesAreIndependentOfCheckerReuse(t *testing.T) {
	t.Parallel()
	node := syntax.ParseAssignedExpression("${{ format('{0}', matrix.value) }}")
	call, ok := node.(*syntax.FuncCallNode)
	if !ok {
		t.Fatal("fixture is not a function call")
	}
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.SetContextAvailability([]string{"matrix"})
	checker.SetContextType("matrix", types.NewStrictObjectType(map[string]types.Type{"value": types.NumberType()}).Type())
	first := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
	checker.SetContextType("matrix", types.NewStrictObjectType(map[string]types.Type{"value": types.BoolType()}).Type())
	second := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
	if !first.Valid || !second.Valid {
		t.Fatalf("invalid fixture: %v / %v", first.Findings, second.Findings)
	}
	for _, tc := range []struct {
		types InferredTypes
		want  types.TypeKind
	}{{first.Types, types.KindNumber}, {second.Types, types.KindBool}} {
		got, known := tc.types.TypeOf(call.Args[1])
		if !known || got.Kind() != tc.want {
			t.Fatalf("argument type=%v known=%t, want %v", got, known, tc.want)
		}
		got, known = tc.types.TypeOf(node)
		if !known || got.Kind() != types.KindString {
			t.Fatalf("format result type=%v known=%t", got, known)
		}
	}
	other := syntax.ParseAssignedExpression("${{ matrix.value }}")
	if _, known := first.Types.TypeOf(other); known {
		t.Fatal("an unchecked node acquired another node's type")
	}
	if _, known := (InferredTypes{}).TypeOf(node); known {
		t.Fatal("empty snapshot reported a type")
	}
}

func TestInferredTypesPreserveInvalidCallReturnContract(t *testing.T) {
	t.Parallel()
	node := syntax.ParseAssignedExpression("${{ startsWith(true) }}")
	result := NewChecker(contexts.DefaultExpressionScope(), nil).CheckResult(evaluate.EvaluateSubexpressions(node))
	got, known := result.Types.TypeOf(node)
	if result.Valid || !known || got.Kind() != types.KindBool {
		t.Fatalf("invalid call lost boolean return type: type=%v known=%t result=%+v", got, known, result)
	}
}

func TestInferredTypesCoverNodesVisitedThroughNarrowing(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"(inputs.enabled && 1) || 2", "(!inputs.enabled || true) && false", "case(inputs.enabled, toJSON(inputs.count), 'fallback')"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			node := syntax.ParseAssignedExpression("${{ " + source + " }}")
			checker := NewChecker(contexts.DefaultExpressionScope(), nil)
			checker.SetContextAvailability([]string{"inputs"})
			contexts.MergeInputTypes(checker, types.NewStrictObjectType(map[string]types.Type{"enabled": types.BoolType(), "count": types.NumberType()}))
			result := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
			if !result.Valid {
				t.Fatal(result.Findings)
			}
			syntax.Visit(node, func(part, _ syntax.Node, entering bool) {
				if !entering {
					return
				}
				ty, known := result.Types.TypeOf(part)
				if !known {
					t.Errorf("missing type for %T at %+v", part, syntax.ExpressionSpan(part))
				}
				if _, negation := part.(*syntax.NotOpNode); negation && ty.Kind() != types.KindBool {
					t.Errorf("negation type=%v", ty)
				}
			})
		})
	}
}
