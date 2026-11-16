package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/model/contexts"
)

func TestFromJSONCompositeConversion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source  string
		invalid bool
	}{
		{"fromJSON(steps.check.outputs).available", true},
		{"fromJSON(steps.check.outputs.available)", false},
		{"fromJSON(true)", false},
		{"fromJSON(42)", false},
		{"false && fromJSON(steps.check.outputs)", false},
		{"fromJSON(matrix.unknown)", false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.env"), nil)
			outputs := types.NewMapObjectType(types.StringType())
			step := types.NewStrictObjectType(map[string]types.Type{"outputs": outputs.Type()})
			checker.SetContextType("steps", types.NewMapObjectType(step.Type()).Type())
			checker.SetContextType("matrix", types.NewMapObjectType(types.AnyType()).Type())
			result := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
			want := 0
			if tc.invalid {
				want = 1
			}
			if len(result.Findings) != want {
				t.Fatalf("findings=%v, want %d", result.Findings, want)
			}
			if want != 0 {
				finding := result.Findings[0]
				if finding.Rule != "expression-call" || finding.Code != "invalid-json" || tc.source[finding.Offset:finding.EndOffset] != "steps.check.outputs" {
					t.Fatalf("unexpected finding: %+v", finding)
				}
			}
		})
	}
}

func TestFromJSONUnknownCollectionMembers(t *testing.T) {
	t.Parallel()
	for _, collection := range []types.Type{
		types.NewArrayType(types.AnyType()).Type(),
		types.NewMapObjectType(types.AnyType()).Type(),
	} {
		t.Run(collection.String(), func(t *testing.T) {
			t.Parallel()
			const source = "fromJSON(matrix.collection)"
			node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.env"), nil)
			checker.SetContextType("matrix", types.NewStrictObjectType(map[string]types.Type{"collection": collection}).Type())
			result := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
			if len(result.Findings) != 1 || result.Findings[0].Rule != "expression-call" || result.Findings[0].Code != "invalid-json" {
				t.Fatalf("expected a JSON parsing failure without a type error: %+v", result.Findings)
			}
		})
	}
}
