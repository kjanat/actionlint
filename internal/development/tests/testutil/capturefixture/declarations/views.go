package declarations

import (
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Step publishes a step in a single-job workflow and returns its declaration view.
func Step(step *definition.Step) workflows.Step {
	view := workflows.Bind("unit-builtin", "", &definition.Workflow{Jobs: map[string]*definition.Job{"test": {Steps: []*definition.Step{step}}}}, false, "").Workflow()
	job, _ := view.Job("test")
	for step := range job.Steps() {
		return step
	}
	return workflows.Step{}
}

// Workflow publishes the supplied workflow and returns its declaration view.
func Workflow(workflow *definition.Workflow) workflows.Workflow {
	return workflows.Bind("unit-builtin", "", workflow, false, "").Workflow()
}

// Job publishes a job in a workflow and returns its declaration view.
func Job(job *definition.Job) workflows.Job {
	view, _ := Workflow(&definition.Workflow{Jobs: map[string]*definition.Job{"test": job}}).Job("test")
	return view
}
