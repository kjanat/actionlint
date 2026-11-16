package action_test

import (
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func ownedActionFixture(action *definition.ExecAction) *definition.ExecAction {
	workflow := workflows.Bind("unit-action", "", &definition.Workflow{Jobs: map[string]*definition.Job{"test": {Steps: []*definition.Step{{Exec: action}}}}}, false, "").Workflow()
	job, _ := workflow.Job("test")
	for step := range job.Steps() {
		action, _ := step.Action()
		return action
	}
	return nil
}
