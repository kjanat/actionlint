package runner_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
)

func runnerJobView(t *testing.T, source string) documentparse.JobView {
	t.Helper()
	content := []byte(source)
	document := documentparse.Workflow("runner.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	var registry documentparse.SourceSnapshots
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
