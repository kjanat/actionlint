package action_test

import (
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func ownedActionFixture(action *workflowlang.ExecAction) workflowdecl.ExecActionView {
	workflow := workflowdecl.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Steps: []*workflowlang.Step{{Exec: action}}}}}, false, "").Bind("unit-action").Workflow()
	job, _ := workflow.Job("test")
	for step := range job.Steps() {
		action, _ := step.Action()
		return action
	}
	return workflowdecl.ExecActionView{}
}
