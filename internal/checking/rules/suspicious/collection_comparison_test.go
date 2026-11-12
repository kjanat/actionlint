package suspicious

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"
)

func TestCollectionComparison(t *testing.T) {
	t.Parallel()
	// https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#operators
	for _, tc := range []struct {
		expression string
		want       int
	}{
		{`fromJSON('{}') == fromJSON('{}')`, 1},
		{`fromJSON('{}') != fromJSON('{}')`, 1},
		{`fromJSON('[]') == fromJSON('[]')`, 1},
		{`fromJSON('[1]') != fromJSON('[1]')`, 1},
		{`github == job`, 1},
		{`github == github`, 0},
		{`github.event == GITHUB['event']`, 0},
		{`matrix['a.b'] == matrix.a.b`, 1},
		{`matrix['a.b'] == MATRIX['A.B']`, 0},
		{`fromJSON('{}') == null`, 0},
		{`null != fromJSON('[]')`, 0},
		{`fromJSON('{}') == fromJSON('[]')`, 0},
		{`fromJSON('42') == fromJSON('42')`, 0},
		{`fromJSON('true') == fromJSON('true')`, 0},
		{`fromJSON('"text"') == fromJSON('"text"')`, 0},
		{`fromJSON('null') == fromJSON('null')`, 0},
		{`fromJSON('{"ready":true}').ready == fromJSON('{"ready":true}').ready`, 0},
		{`fromJSON(inputs.value) == fromJSON(inputs.value)`, 0},
		{`false && (fromJSON('{}') == fromJSON('{}'))`, 0},
		{`true || (fromJSON('[]') != fromJSON('[]'))`, 0},
		{`case(false, fromJSON('{}') == fromJSON('{}'), true)`, 0},
		{`github.event.unknown && (fromJSON('{}') == fromJSON('{}'))`, 1},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			evaluation := expreval.EvaluateSubexpressions(node)
			checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
			checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
			checker.SetContextType("inputs", exprtypes.NewMapObjectType(exprtypes.StringType()).Type())
			object := exprtypes.NewEmptyObjectType().Type()
			checker.SetContextType("matrix", exprtypes.NewObjectType(map[string]exprtypes.Type{
				"a.b": object,
				"a":   exprtypes.NewObjectType(map[string]exprtypes.Type{"b": object}).Type(),
			}).Type())
			result := checker.CheckResult(evaluation)
			if !result.Valid || len(result.Findings) != 0 {
				t.Fatalf("valid expression rejected by type checking: %+v", result)
			}
			findings := CollectionComparison(node, result.Types, evaluation)
			if len(findings) != tc.want {
				t.Fatalf("findings=%v, want %d", findings, tc.want)
			}
			for _, finding := range findings {
				if finding.Rule != "collection-comparison" || finding.Code != "collection-comparison" || finding.EndOffset <= finding.Offset {
					t.Fatalf("incorrect diagnostic: %+v", finding)
				}
			}
		})
	}
}
