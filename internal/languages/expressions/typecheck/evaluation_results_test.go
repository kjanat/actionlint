package typecheck

import (
	"testing"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"
)

type suppliedEvaluation struct {
	expression exprsyntax.Node
	result     exprvalues.EvaluationResult
}

func (supplied suppliedEvaluation) Expression() exprsyntax.Node { return supplied.expression }
func (supplied suppliedEvaluation) Result(node exprsyntax.Node) (exprvalues.EvaluationResult, bool) {
	if node != supplied.expression {
		return exprvalues.EvaluationResult{}, false
	}
	return supplied.result.Clone(), true
}

func TestTypeCheckingConsumesSuppliedEvaluation(t *testing.T) {
	t.Parallel()
	root := exprsyntax.ParseAssignedExpression("${{ contains('x', 'x') }}")
	if root == nil {
		t.Fatal("expression did not parse")
	}
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	result := checker.CheckResult(suppliedEvaluation{expression: root, result: exprvalues.EvaluationResult{State: exprvalues.EvaluationKnown, Value: false}})
	if !result.Valid || result.Type.Kind() != exprtypes.KindBool || result.Evaluation.Value != false || result.Call == nil || result.Call.ReturnDomain != exprfunctions.ReturnDomainContainsValue {
		t.Fatalf("type checking replaced the supplied result: %+v", result)
	}
}
