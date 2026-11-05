package runner_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func runnerJobView(t *testing.T, source string) workflowdecl.JobView {
	t.Helper()
	content := []byte(source)
	document := parsing.Workflow(content)
	if len(document.SyntaxErrors) != 0 {
		t.Fatal(document.SyntaxErrors)
	}
	registry := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := registry.Document("runner.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := snapshot.WorkflowView()
	if !ok {
		t.Fatal("workflow view unavailable")
	}
	job, ok := workflow.Job("test")
	if !ok {
		t.Fatal("test job view unavailable")
	}
	return job
}
