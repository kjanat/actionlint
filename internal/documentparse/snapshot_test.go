package documentparse

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestSourceSnapshotReusesIndexedWorkflow(t *testing.T) {
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/outer\n")
	first := Workflow("ci.yml", content)
	second := Workflow("ci.yml", content)
	var snapshots SourceSnapshots
	snapshot, err := snapshots.Document("ci.yml", content, first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := snapshots.Document("./ci.yml", content, second)
	if err != nil || again != snapshot {
		t.Fatalf("declaration snapshot not reused: %v", err)
	}
	workflow, ok := again.Workflow()
	if !ok || workflow != first.Workflow {
		t.Fatal("traversal did not receive the indexed workflow")
	}
	ref, err := snapshot.StepReference(workflow.Jobs["build"].Steps[0])
	if err != nil || ref.Node == "" || ref.Address != `jobs["build"].steps[0]` {
		t.Fatalf("structural call-site reference: %+v, %v", ref, err)
	}
	if _, err := snapshot.StepReference(second.Workflow.Jobs["build"].Steps[0]); err == nil {
		t.Fatal("accepted a step from an independently decoded AST")
	}
	if _, err := snapshot.StepReference(nil); err == nil {
		t.Fatal("accepted a missing call-site declaration")
	}
}

func TestSourceSnapshotExcludesLookupDeploymentState(t *testing.T) {
	content := []byte("name: outer\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - &shared\n      uses: $/nested\n    - *shared\n")
	document := Action("outer/action.yml", "outer/action.yml", content)
	if !document.CanAnalyze() {
		t.Fatal(document.Findings)
	}
	for _, mappedFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmapped first", true: "mapped first"}[mappedFirst], func(t *testing.T) {
			plain, mapped := *document.Metadata, *document.Metadata
			mapped.SourceMapped = true
			first, second := &plain, &mapped
			if mappedFirst {
				first, second = second, first
			}
			var snapshots SourceSnapshots
			snapshot, err := snapshots.Action("outer/action.yml", first)
			if err != nil {
				t.Fatal(err)
			}
			again, err := snapshots.Action("outer/action.yml", second)
			if err != nil || snapshot != again {
				t.Fatalf("lookups did not share their declaration snapshot: %v", err)
			}
			steps, ok := snapshot.ActionSteps()
			if !ok || len(steps) != 2 {
				t.Fatalf("indexed action declarations: %+v", steps)
			}
			if workflow, ok := snapshot.Workflow(); ok || workflow != nil {
				t.Fatal("action snapshot exposed a workflow traversal input")
			}
			left, err := snapshot.StepReference(steps[0])
			if err != nil {
				t.Fatal(err)
			}
			right, err := snapshot.StepReference(steps[1])
			if err != nil || left.Node == right.Node {
				t.Fatalf("alias ownership collapsed: first=%+v second=%+v error=%v", left, right, err)
			}
			steps[0] = &workflowlang.Step{}
			retained, _ := again.ActionSteps()
			if _, err := again.StepReference(retained[0]); err != nil {
				t.Fatal("consumer changed the snapshot's step sequence")
			}
			if plain.SourceMapped || !mapped.SourceMapped {
				t.Fatal("snapshot reuse changed per-lookup deployment state")
			}
		})
	}
}

func TestSourceSnapshotRejectsRevisionAndKindConflicts(t *testing.T) {
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
	var snapshots SourceSnapshots
	snapshot, err := snapshots.Document("ci.yml", content, Workflow("ci.yml", content))
	if err != nil {
		t.Fatal(err)
	}
	contentCopy := snapshot.Content()
	contentCopy[0] = '!'
	if string(snapshot.Content()) != string(content) {
		t.Fatal("consumer changed the source snapshot")
	}
	changed := append([]byte("# changed\n"), content...)
	if _, err := snapshots.Document("ci.yml", changed, Workflow("ci.yml", changed)); err == nil {
		t.Fatal("accepted conflicting source revisions")
	}
	if _, err := snapshots.Document("ci.yml", content, Action("ci.yml", "ci.yml", content)); err == nil {
		t.Fatal("accepted conflicting declaration kinds")
	}
	if _, err := snapshots.Document("", content, Workflow("ci.yml", content)); err == nil {
		t.Fatal("accepted an empty source identity")
	}
}
