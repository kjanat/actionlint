package declarations

import (
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Step publishes a step in a single-job workflow and returns its declaration view.
func Step(step *workflowlang.Step) workflowdecl.StepView {
	view := workflowdecl.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("unit-builtin").Workflow()
	job, _ := view.Job("test")
	for step := range job.Steps() {
		return step
	}
	return workflowdecl.StepView{}
}

// Action publishes an action step and returns its action declaration view.
func Action(action *workflowlang.ExecAction) workflowdecl.ExecActionView {
	view, _ := Step(&workflowlang.Step{Exec: action}).Action()
	return view
}

// Workflow publishes the supplied workflow and returns its declaration view.
func Workflow(workflow *workflowlang.Workflow) workflowdecl.WorkflowView {
	return workflowdecl.Publish("", workflow, false, "").Bind("unit-builtin").Workflow()
}

// Job publishes a job in a workflow and returns its declaration view.
func Job(job *workflowlang.Job) workflowdecl.JobView {
	view, _ := Workflow(&workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": job}}).Job("test")
	return view
}
