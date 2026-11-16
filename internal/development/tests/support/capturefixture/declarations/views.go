package declarations

import (
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Step publishes a step in a single-job workflow and returns its declaration view.
func Step(step *workflowlang.Step) workflowdecl.Step {
	view := workflowdecl.Bind("unit-builtin", "", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Steps: []*workflowlang.Step{step}}}}, false, "").Workflow()
	job, _ := view.Job("test")
	for step := range job.Steps() {
		return step
	}
	return workflowdecl.Step{}
}

// Workflow publishes the supplied workflow and returns its declaration view.
func Workflow(workflow *workflowlang.Workflow) workflowdecl.Workflow {
	return workflowdecl.Bind("unit-builtin", "", workflow, false, "").Workflow()
}

// Job publishes a job in a workflow and returns its declaration view.
func Job(job *workflowlang.Job) workflowdecl.Job {
	view, _ := Workflow(&workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": job}}).Job("test")
	return view
}
