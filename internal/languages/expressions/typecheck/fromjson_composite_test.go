package typecheck

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"
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
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.env"), nil)
			outputs := exprtypes.NewMapObjectType(exprtypes.StringType())
			step := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"outputs": outputs.Type()})
			checker.SetContextType("steps", exprtypes.NewMapObjectType(step.Type()).Type())
			checker.SetContextType("matrix", exprtypes.NewMapObjectType(exprtypes.AnyType()).Type())
			result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
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
	for _, collection := range []exprtypes.Type{
		exprtypes.NewArrayType(exprtypes.AnyType()).Type(),
		exprtypes.NewMapObjectType(exprtypes.AnyType()).Type(),
	} {
		t.Run(collection.String(), func(t *testing.T) {
			t.Parallel()
			const source = "fromJSON(matrix.collection)"
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.env"), nil)
			checker.SetContextType("matrix", exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"collection": collection}).Type())
			result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if len(result.Findings) != 1 || result.Findings[0].Rule != "expression-call" || result.Findings[0].Code != "invalid-json" {
				t.Fatalf("expected a JSON parsing failure without a type error: %+v", result.Findings)
			}
		})
	}
}
