package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/contexts"
)

func TestStaticReferencesInUnreachableEventBranches(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"github.event_name == 'issues' && noSuchFunction()",
		"github.event_name == 'issues' && startsWith('one argument')",
		"case(github.event_name == 'issues', noSuchFunction(), 'safe')",
		"github.event_name == 'issues' && noSuchContext",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			checker := NewChecker(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"), nil)
			event := exprtypes.NewEventType([]exprtypes.EventVariant{{Event: "push", Payload: exprtypes.NewEmptyStrictObjectType()}})
			contexts.SetEventPayloadType(checker, event)
			node := syntax.ParseAssignedExpression("${{ " + input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			result := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
			if result.Valid || result.FailurePhase != FailureStaticValidation {
				t.Errorf("reference error disappeared with an unreachable branch: %+v", result)
			}
			expressions.CheckOutcome(t, input, map[string]any{"github": map[string]any{"event_name": "push"}}, expressions.Expectation{Failure: expressions.ParseFailure, ExceptionType: "GitHub.Actions.Expressions.ParseException"})
		})
	}
}

func TestUnavailableContextInUnreachableEventBranch(t *testing.T) {
	t.Parallel()
	const input = "case(github.event_name == 'issues', secrets.TOKEN, 'safe')"
	checker := NewChecker(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"), nil)
	contexts.SetEventPayloadType(checker, exprtypes.NewEventType([]exprtypes.EventVariant{{Event: "push", Payload: exprtypes.NewEmptyStrictObjectType()}}))
	node := syntax.ParseAssignedExpression("${{ " + input + " }}")
	if node == nil {
		t.Fatal("expression did not parse")
	}
	result := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
	if result.Valid || result.FailurePhase != FailureStaticValidation {
		t.Fatalf("unavailable context survived validation: %+v", result)
	}
	// The standalone evaluator allows secrets. Workflow validation restricts the
	// contexts accepted in a step condition, independently of its runtime branch.
	expressions.CheckOutcome(t, input, map[string]any{"github": map[string]any{"event_name": "push"}}, expressions.Expectation{Value: "safe"})
	expressions.CheckWorkflow(t, "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - if: ${{ "+input+" }}\n        run: echo ok\n", false)
}
