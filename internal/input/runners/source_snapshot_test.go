package runners

import (
	"reflect"
	"testing"

	runnermodel "actionlint.kjanat.dev/v2/internal/model/runners"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func TestPreparedJobKnowledgeUsesCanonicalSnapshotHandle(t *testing.T) {
	t.Parallel()
	const identity = "ci.yml"
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
	document := parsing.Workflow(content)
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
	if fromView.Platform != runnermodel.MacOrLinux || fromView.Shell != "bash" || !fromView.Ephemeral || !fromView.PathsKnown {
		t.Fatalf("canonical runner facts = %+v", fromView)
	}
	if !reflect.DeepEqual(fromRepeated, fromView) {
		t.Fatalf("repeated canonical handle lost its prepared facts: repeated=%+v view=%+v", fromRepeated, fromView)
	}

	foreignDocument := parsing.Workflow(content)
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
	if got := knowledge.Job(foreignJob); !reflect.DeepEqual(got, runnermodel.JobKnowledge{}) {
		t.Fatalf("foreign registry handle received prepared facts: %+v", got)
	}
	if got := knowledge.Job(workflowdecl.JobView{}); !reflect.DeepEqual(got, runnermodel.JobKnowledge{}) {
		t.Fatalf("zero handle received prepared facts: %+v", got)
	}
}
