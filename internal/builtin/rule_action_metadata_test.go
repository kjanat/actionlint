package builtin

import (
	"context"
	"errors"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestRequireActionMetadataPreservesAnalysisCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	rule := newRuleRequireActionMetadata(ctx, nil)
	cancel()
	step := &workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "actions/checkout@v4", Pos: &workflowlang.Pos{Line: 1, Col: 1}}}}
	if err := rule.VisitStep(ownedStepFixture(step)); !errors.Is(err, context.Canceled) {
		t.Fatalf("analysis cancellation did not reach metadata lookup: %v", err)
	}
	if len(rule.Errs()) != 0 {
		t.Fatal("cancelled lookup produced a metadata availability finding")
	}
}
