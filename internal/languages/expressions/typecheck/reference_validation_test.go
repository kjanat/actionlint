package typecheck

import (
	"testing"

	runnerreference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	contexts "actionlint.kjanat.dev/v2/internal/model/contexts"
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
			node := exprsyntax.ParseAssignedExpression("${{ " + input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if result.Valid || result.FailurePhase != FailureStaticValidation {
				t.Errorf("reference error disappeared with an unreachable branch: %+v", result)
			}
			runnerreference.CheckOutcome(t, input, map[string]any{"github": map[string]any{"event_name": "push"}}, runnerreference.Expectation{Failure: runnerreference.ParseFailure, ExceptionType: "GitHub.Actions.Expressions.ParseException"})
		})
	}
}

func TestUnavailableContextInUnreachableEventBranch(t *testing.T) {
	t.Parallel()
	const input = "case(github.event_name == 'issues', secrets.TOKEN, 'safe')"
	checker := NewChecker(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"), nil)
	contexts.SetEventPayloadType(checker, exprtypes.NewEventType([]exprtypes.EventVariant{{Event: "push", Payload: exprtypes.NewEmptyStrictObjectType()}}))
	node := exprsyntax.ParseAssignedExpression("${{ " + input + " }}")
	if node == nil {
		t.Fatal("expression did not parse")
	}
	result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
	if result.Valid || result.FailurePhase != FailureStaticValidation {
		t.Fatalf("unavailable context survived validation: %+v", result)
	}
	// The standalone evaluator allows secrets. Workflow validation restricts the
	// contexts accepted in a step condition, independently of its runtime branch.
	runnerreference.CheckOutcome(t, input, map[string]any{"github": map[string]any{"event_name": "push"}}, runnerreference.Expectation{Value: "safe"})
	runnerreference.CheckWorkflow(t, "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - if: ${{ "+input+" }}\n        run: echo ok\n", false)
}
