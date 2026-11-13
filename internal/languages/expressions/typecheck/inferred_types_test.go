package typecheck

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestInferredTypesAreIndependentOfCheckerReuse(t *testing.T) {
	t.Parallel()
	node := exprsyntax.ParseAssignedExpression("${{ format('{0}', matrix.value) }}")
	call, ok := node.(*exprsyntax.FuncCallNode)
	if !ok {
		t.Fatal("fixture is not a function call")
	}
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.SetContextAvailability([]string{"matrix"})
	checker.SetContextType("matrix", exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"value": exprtypes.NumberType()}).Type())
	first := checker.CheckResult(expreval.EvaluateSubexpressions(node))
	checker.SetContextType("matrix", exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"value": exprtypes.BoolType()}).Type())
	second := checker.CheckResult(expreval.EvaluateSubexpressions(node))
	if !first.Valid || !second.Valid {
		t.Fatalf("invalid fixture: %v / %v", first.Findings, second.Findings)
	}
	for _, tc := range []struct {
		types InferredTypes
		want  exprtypes.Kind
	}{{first.Types, exprtypes.KindNumber}, {second.Types, exprtypes.KindBool}} {
		got, known := tc.types.TypeOf(call.Args[1])
		if !known || got.Kind() != tc.want {
			t.Fatalf("argument type=%v known=%t, want %v", got, known, tc.want)
		}
		got, known = tc.types.TypeOf(node)
		if !known || got.Kind() != exprtypes.KindString {
			t.Fatalf("format result type=%v known=%t", got, known)
		}
	}
	other := exprsyntax.ParseAssignedExpression("${{ matrix.value }}")
	if _, known := first.Types.TypeOf(other); known {
		t.Fatal("an unchecked node acquired another node's type")
	}
	if _, known := (InferredTypes{}).TypeOf(node); known {
		t.Fatal("empty snapshot reported a type")
	}
}

func TestInferredTypesPreserveInvalidCallReturnContract(t *testing.T) {
	t.Parallel()
	node := exprsyntax.ParseAssignedExpression("${{ startsWith(true) }}")
	result := NewChecker(contextspec.DefaultExpressionScope(), nil).CheckResult(expreval.EvaluateSubexpressions(node))
	got, known := result.Types.TypeOf(node)
	if result.Valid || !known || got.Kind() != exprtypes.KindBool {
		t.Fatalf("invalid call lost boolean return type: type=%v known=%t result=%+v", got, known, result)
	}
}

func TestInferredTypesCoverNodesVisitedThroughNarrowing(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"(inputs.enabled && 1) || 2", "(!inputs.enabled || true) && false", "case(inputs.enabled, toJSON(inputs.count), 'fallback')"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			node := exprsyntax.ParseAssignedExpression("${{ " + source + " }}")
			checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
			checker.SetContextAvailability([]string{"inputs"})
			contextspec.MergeInputTypes(checker, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"enabled": exprtypes.BoolType(), "count": exprtypes.NumberType()}))
			result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if !result.Valid {
				t.Fatal(result.Findings)
			}
			exprsyntax.Visit(node, func(part, _ exprsyntax.Node, entering bool) {
				if !entering {
					return
				}
				ty, known := result.Types.TypeOf(part)
				if !known {
					t.Errorf("missing type for %T at %+v", part, exprsyntax.ExpressionSpan(part))
				}
				if _, negation := part.(*exprsyntax.NotOpNode); negation && ty.Kind() != exprtypes.KindBool {
					t.Errorf("negation type=%v", ty)
				}
			})
		})
	}
}
