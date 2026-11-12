package checking

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

type capturedSnapshots = declarations.CapturedSnapshots

// newCapturedSnapshots returns the registry and capture store for code under
// test and the fixture that admits documents into them.
func newCapturedSnapshots(tb testing.TB, base string) (*parsing.SourceSnapshots, *capture.Store, *capturedSnapshots) {
	tb.Helper()
	return declarations.NewCapturedSnapshots(tb, base)
}

func ownedApplicationStep(step *workflowlang.Step) workflowdecl.StepView {
	workflow := workflowdecl.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"unit": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("unit-application").Workflow()
	job, _ := workflow.Job("unit")
	for step := range job.Steps() {
		return step
	}
	return workflowdecl.StepView{}
}
