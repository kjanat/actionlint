package rules_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func matrixTestWorkflow(t *testing.T, source string) (*workflowlang.Workflow, parsing.SourceSnapshot) {
	t.Helper()
	content := []byte(source)
	document := parsing.Workflow(content)
	if !document.CanAnalyze() {
		t.Fatalf("workflow parse failed: %v", document.SyntaxErrors)
	}
	_, snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("test.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow := document.Workflow
	if workflow == nil {
		t.Fatal("workflow snapshot unavailable")
	}
	return workflow, snapshot
}
