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

func workflowView(t testing.TB, snapshot parsing.SourceSnapshot) workflowdecl.WorkflowView {
	t.Helper()
	view, available := snapshot.WorkflowView()
	if !available {
		t.Fatal("workflow declarations unavailable")
	}
	return view
}

func workflowJob(t testing.TB, workflow workflowdecl.WorkflowView, key string) workflowdecl.JobView {
	t.Helper()
	job, available := workflow.JobsAt(key)
	if !available {
		t.Fatalf("workflow job %q unavailable", key)
	}
	return job
}

func workflowStepViews(t testing.TB, workflow workflowdecl.WorkflowView, key string) []workflowdecl.StepView {
	t.Helper()
	return slices.Collect(workflowJob(t, workflow, key).Steps())
}

func actionStepViews(t testing.TB, snapshot parsing.SourceSnapshot) []workflowdecl.StepView {
	t.Helper()
	action, available := snapshot.ActionView()
	if !available {
		t.Fatal("action declarations unavailable")
	}
	return slices.Collect(action.Steps())
}

func stepReference(t testing.TB, snapshot parsing.SourceSnapshot, step workflowdecl.StepView) source.Reference {
	t.Helper()
	if err := snapshot.ValidateStep(step); err != nil {
		t.Fatal(err)
	}
	return step.Reference()
}

func stepPosition(t testing.TB, step workflowdecl.StepView) workflowlang.Pos {
	t.Helper()
	position, present := step.Pos()
	if !present {
		t.Fatal("step position unavailable")
	}
	return position
}

func sourceFixtureBase(t testing.TB) string { return capturefixture.CurrentDir(t) }

type capturedSnapshots = declarations.CapturedSnapshots

// newCapturedSnapshots returns the registry and capture store for code under
// test and the fixture that admits documents into them.
func newCapturedSnapshots(t testing.TB, base string) (*parsing.SourceSnapshots, *capture.Store, *capturedSnapshots) {
	t.Helper()
	return declarations.NewCapturedSnapshots(t, base)
}
