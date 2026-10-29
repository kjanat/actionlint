package documentparse

import (
	"slices"
	"testing"

	documentmodel "actionlint.kjanat.dev/v2/document"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/source"
)

func TestSourceSnapshotsLookupAndDeclarationRequireExactReference(t *testing.T) {
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n")
	document := Workflow("ci.yml", content)
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Registry() != snapshots.SourceSnapshots {
		t.Fatal("snapshot does not retain its owning registry")
	}
	if found, ok := snapshots.Lookup(snapshot.Reference()); !ok || found != snapshot {
		t.Fatal("exact document lookup did not return its snapshot")
	}
	step := snapshotJobSteps(t, snapshot, "build")[0]
	if err := snapshot.ValidateStep(step); err != nil {
		t.Fatal(err)
	}
	ref := step.Reference()

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
	if declaration, ok := snapshots.Declaration(source.Reference{}); ok || declaration != (workflowdecl.StepView{}) {
		t.Fatal("step lookup accepted a zero reference")
	}
	if declaration, ok := nilSnapshots.Declaration(ref); ok || declaration != (workflowdecl.StepView{}) {
		t.Fatal("step lookup accepted a nil snapshot set")
	}
	if _, ok := snapshots.Declaration(source.Reference{Document: ref.Document}); ok {
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
			if declaration, ok := snapshots.Declaration(modified); ok || declaration != (workflowdecl.StepView{}) {
				t.Fatalf("step lookup accepted modified reference: %+v", modified)
			}
		})
	}
}

func TestSourceSnapshotsRejectRevisionAndKindConflicts(t *testing.T) {
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
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
	workflowSnapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	workflowSnapshot, err := workflowSnapshots.Document("ci.yml", workflowContent, Workflow("ci.yml", workflowContent))
	if err != nil {
		t.Fatal(err)
	}
	build := snapshotJobSteps(t, workflowSnapshot, "build")
	deploy := snapshotJobSteps(t, workflowSnapshot, "deploy")
	outer := build[2]
	parallel, present := outer.Parallel()
	if !present {
		t.Fatal("parallel declaration unavailable")
	}
	nested := slices.Collect(parallel.Steps())
	steps := []workflowdecl.StepView{build[0], build[1], outer, nested[0], nested[1], deploy[0]}
	refs := make([]source.Reference, len(steps))
	for i, step := range steps {
		refs[i] = step.Reference()
		if err := workflowSnapshot.ValidateStep(step); err != nil {
			t.Fatalf("step %d reference: %v", i, err)
		}
		declaration, ok := workflowSnapshots.Declaration(refs[i])
		if !ok || declaration != step || declaration.SourceReference() != workflowSnapshot.Reference() || declaration.Reference() != refs[i] {
			t.Fatalf("step %d declaration does not match its canonical view", i)
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
		declaration, ok := workflowSnapshots.Declaration(refs[i])
		if !ok || declaration.DeclaringJob() != want {
			t.Fatalf("step %d declaring job: got %q, want %q", i, declaration.DeclaringJob(), want)
		}
	}

	actionContent := []byte("name: action\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo action\n")
	actionSnapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	actionSnapshot, err := actionSnapshots.Document("action.yml", actionContent, Action("action.yml", "action.yml", actionContent))
	if err != nil {
		t.Fatal(err)
	}
	actionSteps := snapshotActionSteps(t, actionSnapshot)
	if len(actionSteps) != 1 {
		t.Fatal("action snapshot declarations unavailable")
	}
	actionRef := actionSteps[0].Reference()
	if err := actionSnapshot.ValidateStep(actionSteps[0]); err != nil {
		t.Fatal(err)
	}
	actionDeclaration, ok := actionSnapshots.Declaration(actionRef)
	if !ok || actionDeclaration != actionSteps[0] || actionDeclaration.SourceReference() != actionSnapshot.Reference() || actionDeclaration.Reference() != actionRef || actionDeclaration.DeclaringJob() != "" {
		t.Fatalf("action declaration has unexpected source, view, or job: %+v", actionDeclaration)
	}
}
