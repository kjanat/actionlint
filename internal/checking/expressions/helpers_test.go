package expressions

import (
	"context"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/runneracquisition"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func newTestChecker() *Visitor { return NewVisitor(Dependencies{}, Options{UnsoundTernary: true}) }

func newTestCheckerForSource(t *testing.T, source string) (*Visitor, workflows.BoundWorkflow) {
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
	runners, err := runneracquisition.Prepare(context.Background(), snapshot, nil, runnerapi.Options{}, "test.yml")
	if err != nil {
		t.Fatal(err)
	}
	checker := NewVisitor(Dependencies{Runners: runners}, Options{UnsoundTernary: true})
	return checker, workflow
}

func testJob(tb testing.TB, workflow workflows.BoundWorkflow, key string) workflows.BoundJob {
	tb.Helper()
	job, present := workflow.Job(key)
	if !present {
		tb.Fatalf("fixture job %q absent", key)
	}
	return job
}

func testStep(tb testing.TB, job workflows.BoundJob, index int) workflows.BoundStep {
	tb.Helper()
	steps := slices.Collect(job.Steps())
	if index < 0 || index >= len(steps) {
		tb.Fatalf("fixture step %d absent", index)
	}
	return steps[index]
}

// Pure checker fixtures freeze synthetic typed models; they do not admit bytes
// to a SourceSnapshots registry or claim captured parse provenance.
func ownedWorkflowFixture(workflow *definition.Workflow) workflows.BoundWorkflow {
	return workflows.Bind("unit-expression", "", workflow, false, "").Workflow()
}

func ownedStringFixture(value *definition.String) *definition.String {
	return value
}

func ownedMatrixFixture(matrix *definition.Matrix) *definition.Matrix {
	workflow := ownedWorkflowFixture(&definition.Workflow{Jobs: map[string]*definition.Job{"test": {Strategy: &definition.Strategy{Matrix: matrix}}}})
	job, _ := workflow.Job("test")
	return job.Strategy.Matrix
}

func ownedRawStringFixture(value *definition.RawYAMLString) *definition.RawYAMLString {
	return value
}

func ownedMatrixRowFixture(row *definition.MatrixRow) *definition.MatrixRow {
	return row
}
