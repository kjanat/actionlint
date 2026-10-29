package application

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"

	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
)

func sourceFixtureBase(t testing.TB) string { return capturefixture.CurrentDir(t) }

type capturedSnapshots = declarations.CapturedSnapshots

func newCapturedSnapshots(t testing.TB, base string) *capturedSnapshots {
	t.Helper()
	return declarations.NewCapturedSnapshots(t, base)
}

func ownedApplicationStep(step *workflowlang.Step) workflowdecl.StepView {
	workflow := workflowdecl.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"unit": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("unit-application").Workflow()
	job, _ := workflow.Job("unit")
	for step := range job.Steps() {
		return step
	}
	return workflowdecl.StepView{}
}
