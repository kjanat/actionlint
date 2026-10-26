package expressions

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/runner"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func newTestChecker() *Checker { return New(Dependencies{}, Options{UnsoundTernary: true}) }

func newTestCheckerForSource(t *testing.T, source string) (*Checker, *workflowlang.Workflow) {
	t.Helper()
	content := []byte(source)
	document := documentparse.Workflow("test.yml", content)
	if !document.CanAnalyze() {
		t.Fatalf("workflow parse failed: %v", document.Findings)
	}
	snapshot, err := (&documentparse.SourceSnapshots{}).Document("test.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := snapshot.Workflow()
	if !ok {
		t.Fatal("workflow snapshot unavailable")
	}
	runners, err := runner.Prepare(context.Background(), snapshot, nil, runnerapi.Options{}, "test.yml")
	if err != nil {
		t.Fatal(err)
	}
	checker := New(Dependencies{SourceSnapshot: snapshot, Runners: runners}, Options{UnsoundTernary: true})
	return checker, workflow
}
