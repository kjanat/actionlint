package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/contexts"
)

type suppliedEvaluation struct {
	expression syntax.Node
	result     exprvalues.EvaluationResult
}

func (supplied suppliedEvaluation) Expression() syntax.Node { return supplied.expression }
func (supplied suppliedEvaluation) Result(node syntax.Node) (exprvalues.EvaluationResult, bool) {
	if node != supplied.expression {
		return exprvalues.EvaluationResult{}, false
	}
	return supplied.result.Clone(), true
}

func TestTypeCheckingConsumesSuppliedEvaluation(t *testing.T) {
	t.Parallel()
	root := syntax.ParseAssignedExpression("${{ contains('x', 'x') }}")
	if root == nil {
		t.Fatal("expression did not parse")
	}
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	result := checker.CheckResult(suppliedEvaluation{expression: root, result: exprvalues.EvaluationResult{State: exprvalues.EvaluationKnown, Value: false}})
	if !result.Valid || result.Type.Kind() != exprtypes.KindBool || result.Evaluation.Value != false || result.Call == nil || result.Call.ReturnDomain != functions.ReturnDomainContainsValue {
		t.Fatalf("type checking replaced the supplied result: %+v", result)
	}
}
