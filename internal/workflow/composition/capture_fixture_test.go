package composition

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/source"
)

func workflowView(t testing.TB, snapshot documentparse.SourceSnapshot) workflowdecl.WorkflowView {
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

func actionStepViews(t testing.TB, snapshot documentparse.SourceSnapshot) []workflowdecl.StepView {
	t.Helper()
	action, available := snapshot.ActionView()
	if !available {
		t.Fatal("action declarations unavailable")
	}
	return slices.Collect(action.Steps())
}

func stepReference(t testing.TB, snapshot documentparse.SourceSnapshot, step workflowdecl.StepView) source.Reference {
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

func newCapturedSnapshots(t testing.TB, base string) *capturedSnapshots {
	t.Helper()
	return declarations.NewCapturedSnapshots(t, base)
}
