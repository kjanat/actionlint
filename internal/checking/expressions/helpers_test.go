package expressions

import (
	"context"
	"slices"
	"testing"

	runnerinput "actionlint.kjanat.dev/v2/internal/input/runners"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func newTestChecker() *Checker { return New(Dependencies{}, Options{UnsoundTernary: true}) }

func newTestCheckerForSource(t *testing.T, source string) (*Checker, workflowdecl.Workflow) {
	t.Helper()
	content := []byte(source)
	document := parsing.Workflow(content)
	if !document.CanAnalyze() {
		t.Fatalf("workflow parse failed: %v", document.SyntaxErrors)
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
	runners, err := runnerinput.Prepare(context.Background(), snapshot, nil, runnerapi.Options{}, "test.yml")
	if err != nil {
		t.Fatal(err)
	}
	checker := New(Dependencies{Runners: runners}, Options{UnsoundTernary: true})
	return checker, workflow
}

func testJob(tb testing.TB, workflow workflowdecl.Workflow, key string) workflowdecl.Job {
	tb.Helper()
	job, present := workflow.Job(key)
	if !present {
		tb.Fatalf("fixture job %q absent", key)
	}
	return job
}

func testStep(tb testing.TB, job workflowdecl.Job, index int) workflowdecl.Step {
	tb.Helper()
	steps := slices.Collect(job.Steps())
	if index < 0 || index >= len(steps) {
		tb.Fatalf("fixture step %d absent", index)
	}
	return steps[index]
}

// Pure checker fixtures freeze synthetic typed models; they do not admit bytes
// to a SourceSnapshots registry or claim captured parse provenance.
func ownedWorkflowFixture(workflow *workflowlang.Workflow) workflowdecl.Workflow {
	return workflowdecl.Publish("", workflow, false, "").Bind("unit-expression").Workflow()
}

func ownedStringFixture(value *workflowlang.String) *workflowlang.String {
	return value
}

func ownedMatrixFixture(matrix *workflowlang.Matrix) *workflowlang.Matrix {
	workflow := ownedWorkflowFixture(&workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Strategy: &workflowlang.Strategy{Matrix: matrix}}}})
	job, _ := workflow.Job("test")
	return job.Strategy.Matrix
}

func ownedRawStringFixture(value *workflowlang.RawYAMLString) *workflowlang.RawYAMLString {
	return value
}

func ownedMatrixRowFixture(row *workflowlang.MatrixRow) *workflowlang.MatrixRow {
	return row
}
