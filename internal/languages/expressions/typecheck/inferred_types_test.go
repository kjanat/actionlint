package typecheck

import (
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestInferredTypesAreIndependentOfCheckerReuse(t *testing.T) {
	node := exprsyntax.ParseAssignedExpression("${{ format('{0}', matrix.value) }}")
	call, ok := node.(*exprsyntax.FuncCallNode)
	if !ok {
		t.Fatal("fixture is not a function call")
	}
	checker := NewChecker(nil)
	checker.SetContextAvailability([]string{"matrix"})
	checker.UpdateMatrix(exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"value": exprtypes.NumberType()}))
	first := checker.CheckResult(node)
	checker.UpdateMatrix(exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"value": exprtypes.BoolType()}))
	second := checker.CheckResult(node)
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
	node := exprsyntax.ParseAssignedExpression("${{ startsWith(true) }}")
	result := NewChecker(nil).CheckResult(node)
	got, known := result.Types.TypeOf(node)
	if result.Valid || !known || got.Kind() != exprtypes.KindBool {
		t.Fatalf("invalid call lost boolean return type: type=%v known=%t result=%+v", got, known, result)
	}
}

func TestInferredTypesCoverNodesVisitedThroughNarrowing(t *testing.T) {
	for _, source := range []string{"(inputs.enabled && 1) || 2", "(!inputs.enabled || true) && false", "case(inputs.enabled, toJSON(inputs.count), 'fallback')"} {
		t.Run(source, func(t *testing.T) {
			node := exprsyntax.ParseAssignedExpression("${{ " + source + " }}")
			checker := NewChecker(nil)
			checker.SetContextAvailability([]string{"inputs"})
			checker.UpdateInputs(exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"enabled": exprtypes.BoolType(), "count": exprtypes.NumberType()}))
			result := checker.CheckResult(node)
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
