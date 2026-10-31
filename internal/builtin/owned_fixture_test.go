package builtin

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

func ownedStepFixture(step *workflowlang.Step) workflowdecl.StepView {
	view := workflowdecl.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("unit-builtin").Workflow()
	job, _ := view.Job("test")
	for step := range job.Steps() {
		return step
	}
	return workflowdecl.StepView{}
}

func ownedActionFixture(action *workflowlang.ExecAction) workflowdecl.ExecActionView {
	view, _ := ownedStepFixture(&workflowlang.Step{Exec: action}).Action()
	return view
}

func ownedWorkflowFixture(workflow *workflowlang.Workflow) workflowdecl.WorkflowView {
	return workflowdecl.Publish("", workflow, false, "").Bind("unit-builtin").Workflow()
}

func ownedJobFixture(job *workflowlang.Job) workflowdecl.JobView {
	view, _ := ownedWorkflowFixture(&workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": job}}).Job("test")
	return view
}
