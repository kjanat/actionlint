package composites

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func workflowView(tb testing.TB, snapshot parsing.SourceSnapshot) workflowdecl.WorkflowView {
	tb.Helper()
	view, available := snapshot.WorkflowView()
	if !available {
		tb.Fatal("workflow declarations unavailable")
	}
	return view
}

func workflowJob(tb testing.TB, workflow workflowdecl.WorkflowView, key string) workflowdecl.JobView {
	tb.Helper()
	job, available := workflow.JobsAt(key)
	if !available {
		tb.Fatalf("workflow job %q unavailable", key)
	}
	return job
}

func workflowStepViews(tb testing.TB, workflow workflowdecl.WorkflowView, key string) []workflowdecl.StepView {
	tb.Helper()
	return slices.Collect(workflowJob(tb, workflow, key).Steps())
}

func actionStepViews(tb testing.TB, snapshot parsing.SourceSnapshot) []workflowdecl.StepView {
	tb.Helper()
	action, available := snapshot.ActionView()
	if !available {
		tb.Fatal("action declarations unavailable")
	}
	return slices.Collect(action.Steps())
}

func stepReference(tb testing.TB, snapshot parsing.SourceSnapshot, step workflowdecl.StepView) source.Reference {
	tb.Helper()
	if err := snapshot.ValidateStep(step); err != nil {
		tb.Fatal(err)
	}
	return step.Reference()
}

func stepPosition(tb testing.TB, step workflowdecl.StepView) workflowlang.Pos {
	tb.Helper()
	position, present := step.Pos()
	if !present {
		tb.Fatal("step position unavailable")
	}
	return position
}

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
