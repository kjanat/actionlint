package declarations

import (
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func Step(step *definition.Step) workflows.BoundStep {
	view := workflows.Bind("unit-builtin", "", &definition.Workflow{Jobs: map[string]*definition.Job{"test": {Steps: []*definition.Step{step}}}}, false, "").Workflow()
	job, _ := view.Job("test")
	for step := range job.Steps() {
		return step
	}
	return workflows.BoundStep{}
}

func Workflow(workflow *definition.Workflow) workflows.BoundWorkflow {
	return workflows.Bind("unit-builtin", "", workflow, false, "").Workflow()
}

func Job(job *definition.Job) workflows.BoundJob {
	view, _ := Workflow(&definition.Workflow{Jobs: map[string]*definition.Job{"test": job}}).Job("test")
	return view
}
