package composites

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

func workflowView(tb testing.TB, snapshot parsing.SourceSnapshot) workflows.BoundWorkflow {
	tb.Helper()
	view, available := snapshot.WorkflowView()
	if !available {
		tb.Fatal("workflow declarations unavailable")
	}
	return view
}

func workflowJob(tb testing.TB, workflow workflows.BoundWorkflow, key string) workflows.BoundJob {
	tb.Helper()
	job, available := workflow.Job(key)
	if !available {
		tb.Fatalf("workflow job %q unavailable", key)
	}
	return job
}

func workflowStepViews(tb testing.TB, workflow workflows.BoundWorkflow, key string) []workflows.BoundStep {
	tb.Helper()
	return slices.Collect(workflowJob(tb, workflow, key).Steps())
}

func actionStepViews(tb testing.TB, snapshot parsing.SourceSnapshot) []workflows.BoundStep {
	tb.Helper()
	action, available := snapshot.ActionView()
	if !available {
		tb.Fatal("action declarations unavailable")
	}
	return slices.Collect(action.Steps())
}

func stepReference(tb testing.TB, snapshot parsing.SourceSnapshot, step workflows.BoundStep) sourcetext.Reference {
	tb.Helper()
	if err := snapshot.ValidateStep(step); err != nil {
		tb.Fatal(err)
	}
	return step.Reference()
}

func stepPosition(tb testing.TB, step workflows.BoundStep) sourcetext.Position {
	tb.Helper()
	position, present := step.Position()
	if !present {
		tb.Fatal("step position unavailable")
	}
	return position
}

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

func newCapturedSnapshots(tb testing.TB, base string) (*parsing.SourceSnapshots, *capture.Store, *declarations.CapturedSnapshots) {
	tb.Helper()
	return declarations.NewCapturedSnapshots(tb, base)
}
