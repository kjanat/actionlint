package runner

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func TestNilKnowledgeUsesCanonicalJobFallback(t *testing.T) {
	t.Parallel()
	const identity = "ci.yml"
	content := []byte("on: push\njobs:\n  test:\n    runs-on: windows-latest\n    steps: [{run: echo ok}]\n")
	document := parsing.Workflow(content)
	registry := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := registry.Document(identity, content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, available := snapshot.WorkflowView()
	if !available {
		t.Fatal("workflow view is unavailable")
	}
	job, available := workflow.Job("test")
	if !available {
		t.Fatal("canonical job is unavailable")
	}
	var missing *Knowledge
	got := missing.Job(job)
	if got.Platform != Windows || got.Shell != "pwsh" || !got.CaseInsensitive || !got.Ephemeral || !got.PathsKnown {
		t.Fatalf("nil knowledge fallback = %+v", got)
	}
}
