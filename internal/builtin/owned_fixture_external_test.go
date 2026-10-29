package builtin_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

func ownedWorkflowFixture(workflow *workflowlang.Workflow) workflowdecl.WorkflowView {
	return workflowdecl.Publish("", workflow, false, "").Bind("unit-builtin").Workflow()
}

func ownedJobFixture(job *workflowlang.Job) workflowdecl.JobView {
	view, _ := ownedWorkflowFixture(&workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": job}}).Job("test")
	return view
}

func ownedStepFixture(step *workflowlang.Step) workflowdecl.StepView {
	for step := range ownedJobFixture(&workflowlang.Job{Steps: []*workflowlang.Step{step}}).Steps() {
		return step
	}
	return workflowdecl.StepView{}
}

func matrixJob(t testing.TB, snapshot *documentparse.SourceSnapshot, key string) workflowdecl.JobView {
	t.Helper()
	workflow, present := snapshot.WorkflowView()
	if !present {
		t.Fatal("captured workflow absent")
	}
	job, present := workflow.Job(key)
	if !present {
		t.Fatalf("captured job %q absent", key)
	}
	return job
}
