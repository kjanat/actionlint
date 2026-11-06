package typecheck_test

import (
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprcheck "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	contexts "actionlint.kjanat.dev/v2/internal/model/contexts"
)

func TestStaticCallsInUnreachableEventBranches(t *testing.T) {
	for _, input := range []string{
		"github.event_name == 'issues' && noSuchFunction()",
		"github.event_name == 'issues' && startsWith('one argument')",
		"case(github.event_name == 'issues', noSuchFunction(), 'safe')",
	} {
		t.Run(input, func(t *testing.T) {
			checker := exprcheck.NewChecker(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"), nil)
			event := exprtypes.NewEventType([]exprtypes.EventVariant{{Event: "push", Payload: exprtypes.NewEmptyStrictObjectType()}})
			contexts.SetEventPayloadType(checker, event)
			node := exprsyntax.ParseAssignedExpression("${{ " + input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			result := checker.CheckResult(node)
			if result.Valid || result.FailurePhase != exprcheck.FailureStaticValidation {
				t.Errorf("call declaration error disappeared with an unreachable branch: %+v", result)
			}
		})
	}
}
