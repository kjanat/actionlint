package runner

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func TestPreparedJobKnowledgeUsesCanonicalSnapshotHandle(t *testing.T) {
	const identity = "ci.yml"
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
	document := documentparse.Workflow(identity, content)
	registry := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := registry.Document(identity, content, document)
	if err != nil {
		t.Fatal(err)
	}
	view, available := snapshot.WorkflowView()
	if !available {
		t.Fatal("workflow view is unavailable")
	}
	job, available := view.Job("test")
	if !available {
		t.Fatal("canonical job is unavailable")
	}
	knowledge, err := Prepare(t.Context(), snapshot, nil, runnerapi.Options{}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	fromView := knowledge.Job(job)
	repeated, available := view.Job("test")
	if !available {
		t.Fatal("canonical job disappeared")
	}
	fromRepeated := knowledge.Job(repeated)
	if fromView.Platform != MacOrLinux || fromView.Shell != "bash" || !fromView.Ephemeral || !fromView.PathsKnown {
		t.Fatalf("canonical runner facts = %+v", fromView)
	}
	if !reflect.DeepEqual(fromRepeated, fromView) {
		t.Fatalf("repeated canonical handle lost its prepared facts: repeated=%+v view=%+v", fromRepeated, fromView)
	}

	foreignDocument := documentparse.Workflow(identity, content)
	foreignRegistry := newCapturedSnapshots(t, sourceFixtureBase(t))
	foreignSnapshot, err := foreignRegistry.Document(identity, content, foreignDocument)
	if err != nil {
		t.Fatal(err)
	}
	foreignWorkflow, available := foreignSnapshot.WorkflowView()
	if !available {
		t.Fatal("foreign workflow view is unavailable")
	}
	foreignJob, available := foreignWorkflow.Job("test")
	if !available {
		t.Fatal("foreign canonical job is unavailable")
	}
	if got := knowledge.Job(foreignJob); !reflect.DeepEqual(got, JobKnowledge{}) {
		t.Fatalf("foreign registry handle received prepared facts: %+v", got)
	}
	if got := knowledge.Job(workflowdecl.JobView{}); !reflect.DeepEqual(got, JobKnowledge{}) {
		t.Fatalf("zero handle received prepared facts: %+v", got)
	}
}

func TestNilKnowledgeUsesCanonicalJobFallback(t *testing.T) {
	const identity = "ci.yml"
	content := []byte("on: push\njobs:\n  test:\n    runs-on: windows-latest\n    steps: [{run: echo ok}]\n")
	document := documentparse.Workflow(identity, content)
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
