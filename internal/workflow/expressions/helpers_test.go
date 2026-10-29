package expressions

import (
	"context"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/runner"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func newTestChecker() *Checker { return New(Dependencies{}, Options{UnsoundTernary: true}) }

func newTestCheckerForSource(t *testing.T, source string) (*Checker, workflowdecl.WorkflowView) {
	t.Helper()
	content := []byte(source)
	document := documentparse.Workflow("test.yml", content)
	if !document.CanAnalyze() {
		t.Fatalf("workflow parse failed: %v", document.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("test.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := snapshot.WorkflowView()
	if !ok {
		t.Fatal("workflow snapshot unavailable")
	}
	runners, err := runner.Prepare(context.Background(), snapshot, nil, runnerapi.Options{}, "test.yml")
	if err != nil {
		t.Fatal(err)
	}
	checker := New(Dependencies{Runners: runners}, Options{UnsoundTernary: true})
	return checker, workflow
}

func testJob(t testing.TB, workflow workflowdecl.WorkflowView, key string) workflowdecl.JobView {
	t.Helper()
	job, present := workflow.Job(key)
	if !present {
		t.Fatalf("fixture job %q absent", key)
	}
	return job
}

func testStep(t testing.TB, job workflowdecl.JobView, index int) workflowdecl.StepView {
	t.Helper()
	steps := slices.Collect(job.Steps())
	if index < 0 || index >= len(steps) {
		t.Fatalf("fixture step %d absent", index)
	}
	return steps[index]
}

// Pure checker fixtures freeze synthetic typed models; they do not admit bytes
// to a SourceSnapshots registry or claim captured parse provenance.
func ownedWorkflowFixture(workflow *workflowlang.Workflow) workflowdecl.WorkflowView {
	return workflowdecl.Publish("", workflow, false, "").Bind("unit-expression").Workflow()
}

func ownedStringFixture(value *workflowlang.String) workflowdecl.StringView {
	return ownedWorkflowFixture(&workflowlang.Workflow{Name: value}).Name()
}

func ownedMatrixFixture(matrix *workflowlang.Matrix) workflowdecl.MatrixView {
	workflow := ownedWorkflowFixture(&workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Strategy: &workflowlang.Strategy{Matrix: matrix}}}})
	job, _ := workflow.Job("test")
	return job.Strategy().Matrix()
}

func ownedRawStringFixture(value *workflowlang.RawYAMLString) workflowdecl.RawYAMLStringView {
	row, _ := ownedMatrixFixture(&workflowlang.Matrix{Rows: map[string]*workflowlang.MatrixRow{"value": {Values: []workflowlang.RawYAMLValue{value}}}}).RowsAt("value")
	for value := range row.Values() {
		scalar, _ := value.RawYAMLString()
		return scalar
	}
	return workflowdecl.RawYAMLStringView{}
}

func ownedMatrixRowFixture(row *workflowlang.MatrixRow) workflowdecl.MatrixRowView {
	view, _ := ownedMatrixFixture(&workflowlang.Matrix{Rows: map[string]*workflowlang.MatrixRow{"value": row}}).RowsAt("value")
	return view
}
