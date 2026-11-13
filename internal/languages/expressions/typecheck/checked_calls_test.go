package typecheck

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestCheckedCallsDescribeNestedDemandAndRejectedSignatures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source  string
		demand  exprfunctions.ArgumentDemand
		matched bool
	}{
		{"startsWith(true, 't')", exprfunctions.DemandRequired, true},
		{"startsWith(true)", exprfunctions.DemandRequired, false},
		{"case(false, startsWith(true, 't'), false)", exprfunctions.DemandSkipped, true},
		{"case(inputs.flag, startsWith(true, 't'), false)", exprfunctions.DemandConditional, true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			root := exprsyntax.ParseAssignedExpression("${{ " + tc.source + " }}")
			checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
			contextspec.MergeInputTypes(checker, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"flag": exprtypes.BoolType()}))
			result := checker.CheckResult(expreval.EvaluateSubexpressions(root))
			found := false
			exprsyntax.Visit(root, func(node, _ exprsyntax.Node, entering bool) {
				call, ok := node.(*exprsyntax.FuncCallNode)
				if !entering || !ok || call.Callee != "startsWith" {
					return
				}
				facts, known := result.Calls.CallOf(call)
				if !known || facts.SignatureMatched != tc.matched || facts.Demand != tc.demand {
					t.Fatalf("call facts=%+v known=%t", facts, known)
				}
				if len(facts.DeclaredReturns) != 1 || facts.DeclaredReturns[0].Kind() != exprtypes.KindBool {
					t.Fatalf("declared return lost: %+v", facts)
				}
				found = true
			})
			if !found {
				t.Fatal("fixture has no target call")
			}
		})
	}
}

func TestCheckedCallsDetachValuesAndSurviveCheckerReuse(t *testing.T) {
	t.Parallel()
	root := exprsyntax.ParseAssignedExpression(`${{ contains(fromJSON('[{"items":["original"]}]'), 'x') }}`)
	call, ok := root.(*exprsyntax.FuncCallNode)
	if !ok {
		t.Fatal("fixture is not a call")
	}
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	first := checker.CheckResult(expreval.EvaluateSubexpressions(root))
	if !first.Valid || first.Call == nil {
		t.Fatalf("invalid fixture: %+v", first)
	}
	facts, known := first.Calls.CallOf(call)
	if !known {
		t.Fatal("missing call")
	}
	change := func(value any) {
		t.Helper()
		array, ok := value.([]any)
		if !ok || len(array) != 1 {
			t.Fatalf("unexpected array: %v", value)
		}
		object, ok := array[0].(map[string]any)
		if !ok {
			t.Fatal("missing object")
		}
		items, ok := object["items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatal("missing items")
		}
		items[0] = "changed"
	}
	change(facts.Arguments[0].SourceValue.Value)
	change(facts.Arguments[0].Value.Value)
	change(first.Call.Arguments[0].SourceValue.Value)
	first.Call.DeclaredReturns[0] = exprtypes.StringType()
	facts.DeclaredReturns[0] = exprtypes.NumberType()
	checker.CheckResult(expreval.EvaluateSubexpressions(exprsyntax.ParseAssignedExpression("${{ always() }}")))
	again, known := first.Calls.CallOf(call)
	if !known || again.DeclaredReturns[0].Kind() != exprtypes.KindBool {
		t.Fatalf("call snapshot changed: %+v", again)
	}
	for _, value := range []any{again.Arguments[0].SourceValue.Value, again.Arguments[0].Value.Value} {
		array := value.([]any)
		if got := array[0].(map[string]any)["items"].([]any)[0]; got != "original" {
			t.Fatalf("aliased runtime value: %v", got)
		}
	}
	other := exprsyntax.ParseAssignedExpression("${{ contains('x', 'x') }}").(*exprsyntax.FuncCallNode)
	if _, known := first.Calls.CallOf(other); known {
		t.Fatal("unchecked call acquired facts")
	}
	if _, known := (CheckedCalls{}).CallOf(call); known {
		t.Fatal("empty snapshot reported call facts")
	}
}
