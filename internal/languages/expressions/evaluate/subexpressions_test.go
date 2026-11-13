package evaluate

import (
	"reflect"
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func TestSubexpressionResultsPreserveDemandedRootOutcome(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"false && fromJSON('bad')",
		"true || fromJSON(null)",
		"format('plain', fromJSON(null))",
		"format('{1}', fromJSON(null), 'selected')",
		"format('{1}{0}', fromJSON(null), fromJSON('bad'))",
		"case(false, fromJSON('bad'), 'safe')",
		"case(github.event_name == 'push', 'a', 'b')",
		"fromJSON('{\"nested\":[1,2]}').nested[0] == 1",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			root := exprsyntax.ParseAssignedExpression("${{ " + source + " }}")
			if root == nil {
				t.Fatal("expression did not parse")
			}
			results := EvaluateSubexpressions(root)
			result, found := results.Result(root)
			if !found || !reflect.DeepEqual(result, Evaluate(root, nil)) {
				t.Fatalf("recorded root=%+v found=%t, direct=%+v", result, found, Evaluate(root, nil))
			}
			exprsyntax.Visit(root, func(node, _ exprsyntax.Node, entering bool) {
				if entering {
					return
				}
				got, exists := results.Result(node)
				if exists && !reflect.DeepEqual(got, Evaluate(node, nil)) {
					t.Errorf("node %T: recorded=%+v found=%t, direct=%+v", node, got, exists, Evaluate(node, nil))
				}
			})
		})
	}
}

func TestSubexpressionResultsSkipUnusedBranches(t *testing.T) {
	t.Parallel()
	root := exprsyntax.ParseAssignedExpression("${{ false && fromJSON('bad') }}")
	if root == nil {
		t.Fatal("expression did not parse")
	}
	logical, ok := root.(*exprsyntax.LogicalOpNode)
	if !ok {
		t.Fatalf("unexpected node %T", root)
	}
	results := EvaluateSubexpressions(root)
	parent, _ := results.Result(root)
	child, found := results.Result(logical.Right)
	if parent.State != exprvalues.EvaluationKnown || parent.Value != false || found {
		t.Fatalf("parent=%+v child=%+v found=%t", parent, child, found)
	}
}

func TestSubexpressionResultsDetachCompositeValues(t *testing.T) {
	t.Parallel()
	root := exprsyntax.ParseAssignedExpression("${{ fromJSON('{\"nested\":[\"original\"]}').nested }}")
	if root == nil {
		t.Fatal("expression did not parse")
	}
	property, ok := root.(*exprsyntax.ObjectDerefNode)
	if !ok {
		t.Fatalf("unexpected node %T", root)
	}
	results := EvaluateSubexpressions(root)
	if results.Expression() != root {
		t.Fatal("recorded results identify another syntax tree")
	}
	objectResult, _ := results.Result(property.Receiver)
	object, ok := objectResult.Value.(map[string]any)
	if !ok {
		t.Fatalf("unexpected value %T", objectResult.Value)
	}
	items, ok := object["nested"].([]any)
	if !ok {
		t.Fatalf("unexpected array %T", object["nested"])
	}
	items[0] = "changed"
	result, _ := results.Result(root)
	if !reflect.DeepEqual(result.Value, []any{"original"}) {
		t.Fatalf("nested mutation changed the recorded result: %+v", result)
	}
	other := exprsyntax.ParseAssignedExpression("${{ null }}")
	if _, found := results.Result(other); found {
		t.Fatal("another tree's node has a recorded result")
	}
}

func TestSubexpressionResultsFollowFunctionDemand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source   string
		recorded []bool
	}{
		{"format('plain', fromJSON(null))", []bool{true, false}},
		{"external('value')", []bool{true}},
		{"startsWith(fromJSON('{}'), fromJSON(null))", []bool{true, false}},
		{"case(inputs.enabled, 'first', true, 'second', fromJSON('bad'))", []bool{true, true, true, true, false}},
		{"case(inputs.enabled, fromJSON('bad'), 'safe')", []bool{true, true, true}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			root := exprsyntax.ParseAssignedExpression("${{ " + tc.source + " }}")
			if root == nil {
				t.Fatal("expression did not parse")
			}
			call, ok := root.(*exprsyntax.FuncCallNode)
			if !ok {
				t.Fatalf("unexpected node %T", root)
			}
			results := EvaluateSubexpressions(root)
			for index, arg := range call.Args {
				if _, found := results.Result(arg); found != tc.recorded[index] {
					t.Errorf("argument %d recorded=%t, want %t", index, found, tc.recorded[index])
				}
			}
		})
	}
}
