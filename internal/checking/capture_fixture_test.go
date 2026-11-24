package checking

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

func newCapturedSnapshots(tb testing.TB, base string) (*parsing.SourceSnapshots, *capture.Store, *declarations.CapturedSnapshots) {
	tb.Helper()
	return declarations.NewCapturedSnapshots(tb, base)
}

func boundApplicationStep(step *definition.Step) workflows.BoundStep {
	workflow := workflows.Bind("unit-application", "", &definition.Workflow{Jobs: map[string]*definition.Job{"unit": {Steps: []*definition.Step{step}}}}, false, "").Workflow()
	job, _ := workflow.Job("unit")
	for step := range job.Steps() {
		return step
	}
	return workflows.BoundStep{}
}
