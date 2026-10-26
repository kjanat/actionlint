package documentparse

import (
	"testing"

	documentmodel "actionlint.kjanat.dev/v2/document"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/source"
)

func TestSourceSnapshotsLookupAndStepRequireExactReference(t *testing.T) {
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n")
	document := Workflow("ci.yml", content)
	var snapshots SourceSnapshots
	snapshot, err := snapshots.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Registry() != &snapshots {
		t.Fatal("snapshot does not retain its owning registry")
	}
	if found, ok := snapshots.Lookup(snapshot.Reference()); !ok || found != snapshot {
		t.Fatal("exact document lookup did not return its snapshot")
	}
	workflow, ok := snapshot.Workflow()
	if !ok {
		t.Fatal("workflow snapshot unavailable")
	}
	step := workflow.Jobs["build"].Steps[0]
	ref, err := snapshot.StepReference(step)
	if err != nil {
		t.Fatal(err)
	}

	if got, ok := snapshots.Lookup(source.Reference{}); ok || got != nil {
		t.Fatal("lookup accepted a zero reference")
	}
	var nilSnapshots *SourceSnapshots
	if got, ok := nilSnapshots.Lookup(ref); ok || got != nil {
		t.Fatal("lookup accepted a nil snapshot set")
	}
	if _, ok := snapshots.Lookup(source.Reference{Document: ref.Document}); ok {
		t.Fatal("lookup accepted a missing reference")
	}
	if declaration, ok := snapshots.Step(source.Reference{}); ok || declaration.Source != nil || declaration.Step != nil {
		t.Fatal("step lookup accepted a zero reference")
	}
	if declaration, ok := nilSnapshots.Step(ref); ok || declaration.Source != nil || declaration.Step != nil {
		t.Fatal("step lookup accepted a nil snapshot set")
	}
	if _, ok := snapshots.Step(source.Reference{Document: ref.Document}); ok {
		t.Fatal("step lookup accepted a missing reference")
	}

	mutations := map[string]func(*source.Reference){
		"document": func(value *source.Reference) { value.Document += ".other" },
		"revision": func(value *source.Reference) { value.Revision += ".other" },
		"node":     func(value *source.Reference) { value.Node += ".other" },
		"address":  func(value *source.Reference) { value.Address += ".other" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			modified := ref
			mutate(&modified)
			documentRef := snapshot.Reference()
			mutate(&documentRef)
			if got, ok := snapshots.Lookup(documentRef); ok || got != nil {
				t.Fatalf("lookup accepted modified document reference: %+v", documentRef)
			}
			if declaration, ok := snapshots.Step(modified); ok || declaration.Source != nil || declaration.Step != nil {
				t.Fatalf("step lookup accepted modified reference: %+v", modified)
			}
		})
	}
}

func TestSourceSnapshotsRejectRevisionAndKindConflicts(t *testing.T) {
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
	var snapshots SourceSnapshots
	if _, err := snapshots.Document("ci.yml", content, Workflow("ci.yml", content)); err != nil {
		t.Fatal(err)
	}
	changed := append([]byte("# changed\n"), content...)
	if _, err := snapshots.Document("ci.yml", changed, Workflow("ci.yml", changed)); err == nil {
		t.Fatal("accepted a conflicting source revision")
	}
	if _, err := snapshots.Document("ci.yml", content, Document{Outline: documentmodel.Action{}}); err == nil {
		t.Fatal("accepted conflicting declaration kinds")
	}
}

func TestSourceSnapshotsIndexWorkflowAndActionStepJobs(t *testing.T) {
	workflowContent := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - &shared\n        run: echo alias\n      - *shared\n      - id: outer\n        parallel:\n          - run: echo first\n          - uses: actions/checkout@v4\n  Deploy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo deploy\n")
	var workflowSnapshots SourceSnapshots
	workflowSnapshot, err := workflowSnapshots.Document("ci.yml", workflowContent, Workflow("ci.yml", workflowContent))
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := workflowSnapshot.Workflow()
	if !ok {
		t.Fatal("workflow snapshot unavailable")
	}
	build := workflow.Jobs["build"]
	deploy := workflow.Jobs["deploy"]
	outer := build.Steps[2]
	parallel := outer.Exec.(*workflowlang.ExecParallel)
	steps := []*workflowlang.Step{build.Steps[0], build.Steps[1], outer, parallel.Steps[0], parallel.Steps[1], deploy.Steps[0]}
	refs := make([]source.Reference, len(steps))
	for i, step := range steps {
		refs[i], err = workflowSnapshot.StepReference(step)
		if err != nil {
			t.Fatalf("step %d reference: %v", i, err)
		}
		declaration, ok := workflowSnapshots.Step(refs[i])
		if !ok || declaration.Source != workflowSnapshot || declaration.Step != step {
			t.Fatalf("step %d declaration does not match indexed AST", i)
		}
	}
	for i, ref := range refs {
		for j := range i {
			if ref == refs[j] {
				t.Fatalf("workflow steps %d and %d share a reference", i, j)
			}
		}
	}
	for i, want := range []string{"Build", "Build", "Build", "Build", "Build", "Deploy"} {
		declaration, ok := workflowSnapshots.Step(refs[i])
		if !ok || declaration.Job != want {
			t.Fatalf("step %d declaring job: got %q, want %q", i, declaration.Job, want)
		}
	}

	actionContent := []byte("name: action\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo action\n")
	var actionSnapshots SourceSnapshots
	actionSnapshot, err := actionSnapshots.Document("action.yml", actionContent, Action("action.yml", "action.yml", actionContent))
	if err != nil {
		t.Fatal(err)
	}
	actionSteps, ok := actionSnapshot.ActionSteps()
	if !ok || len(actionSteps) != 1 {
		t.Fatal("action snapshot declarations unavailable")
	}
	actionRef, err := actionSnapshot.StepReference(actionSteps[0])
	if err != nil {
		t.Fatal(err)
	}
	actionDeclaration, ok := actionSnapshots.Step(actionRef)
	if !ok || actionDeclaration.Source != actionSnapshot || actionDeclaration.Step != actionSteps[0] || actionDeclaration.Job != "" {
		t.Fatalf("action declaration has unexpected source, AST, or job: %+v", actionDeclaration)
	}
}
