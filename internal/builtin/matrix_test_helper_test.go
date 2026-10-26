package builtin_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func matrixTestWorkflow(t *testing.T, source string) (*workflowlang.Workflow, *documentparse.SourceSnapshot) {
	t.Helper()
	content := []byte(source)
	document := documentparse.Workflow("test.yml", content)
	if !document.CanAnalyze() {
		t.Fatalf("workflow parse failed: %v", document.Findings)
	}
	snapshots := &documentparse.SourceSnapshots{}
	snapshot, err := snapshots.Document("test.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := snapshot.Workflow()
	if !ok {
		t.Fatal("workflow snapshot unavailable")
	}
	return workflow, snapshot
}
