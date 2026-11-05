package conventions

import (
	"context"
	"errors"
	"testing"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestRequireActionMetadataPreservesAnalysisCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	rule := NewRuleRequireActionMetadata(ctx, nil)
	cancel()
	step := &workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "actions/checkout@v4", Pos: &workflowlang.Pos{Line: 1, Col: 1}}}}
	snapshot := workflowdecl.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("metadata-cancellation").Workflow()
	job, _ := snapshot.Job("test")
	var checkedStep workflowdecl.StepView
	for step := range job.Steps() {
		checkedStep = step
		break
	}
	if err := rule.VisitStep(checkedStep); !errors.Is(err, context.Canceled) {
		t.Fatalf("analysis cancellation did not reach metadata lookup: %v", err)
	}
	if len(rule.Errs()) != 0 {
		t.Fatal("cancelled lookup produced a metadata availability finding")
	}
}
