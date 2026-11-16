package workflow_test

import (
	"slices"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestAllStepsYieldsGroupsBeforeTheirSteps(t *testing.T) {
	t.Parallel()
	step := func(id string, exec workflowlang.Exec) *workflowlang.Step {
		return &workflowlang.Step{ID: &workflowlang.String{Value: id}, Exec: exec}
	}
	steps := []*workflowlang.Step{
		step("a", &workflowlang.ExecRun{}),
		nil,
		step("group", &workflowlang.ExecParallel{Steps: []*workflowlang.Step{
			step("b", &workflowlang.ExecRun{}),
			nil,
			step("inner", &workflowlang.ExecParallel{Steps: []*workflowlang.Step{step("c", &workflowlang.ExecRun{})}}),
			step("d", &workflowlang.ExecRun{}),
		}}),
		step("empty", (*workflowlang.ExecParallel)(nil)),
		step("e", &workflowlang.ExecRun{}),
	}
	var ids []string
	for step := range workflowlang.AllSteps(steps) {
		ids = append(ids, step.ID.Value)
	}
	if want := []string{"a", "group", "b", "inner", "c", "d", "empty", "e"}; !slices.Equal(ids, want) {
		t.Fatalf("AllSteps = %v, want %v", ids, want)
	}
	ids = nil
	for step := range workflowlang.AllSteps(steps) {
		ids = append(ids, step.ID.Value)
		if step.ID.Value == "c" {
			break
		}
	}
	if want := []string{"a", "group", "b", "inner", "c"}; !slices.Equal(ids, want) {
		t.Fatalf("AllSteps after break = %v, want %v", ids, want)
	}
}
