package definition_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestAllStepsYieldsGroupsBeforeTheirSteps(t *testing.T) {
	t.Parallel()
	step := func(id string, exec definition.Exec) *definition.Step {
		return &definition.Step{ID: &definition.String{Value: id}, Exec: exec}
	}
	steps := []*definition.Step{
		step("a", &definition.ExecRun{}),
		nil,
		step("group", &definition.ExecParallel{Steps: []*definition.Step{
			step("b", &definition.ExecRun{}),
			nil,
			step("inner", &definition.ExecParallel{Steps: []*definition.Step{step("c", &definition.ExecRun{})}}),
			step("d", &definition.ExecRun{}),
		}}),
		step("empty", (*definition.ExecParallel)(nil)),
		step("e", &definition.ExecRun{}),
	}
	var ids []string
	for step := range definition.AllSteps(steps) {
		ids = append(ids, step.ID.Value)
	}
	if want := []string{"a", "group", "b", "inner", "c", "d", "empty", "e"}; !slices.Equal(ids, want) {
		t.Fatalf("AllSteps = %v, want %v", ids, want)
	}
	ids = nil
	for step := range definition.AllSteps(steps) {
		ids = append(ids, step.ID.Value)
		if step.ID.Value == "c" {
			break
		}
	}
	if want := []string{"a", "group", "b", "inner", "c"}; !slices.Equal(ids, want) {
		t.Fatalf("AllSteps after break = %v, want %v", ids, want)
	}
}
