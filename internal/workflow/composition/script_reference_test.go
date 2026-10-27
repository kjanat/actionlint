package composition

import (
	"path/filepath"
	"reflect"
	"testing"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/source"
)

func TestAliasedScriptsKeepDeclarationReferences(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - &shared\n        run: echo ok\n      - *shared\n")
	document := documentparse.Workflow("ci.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	service := New(Options{Context: t.Context(), SourceSnapshots: snapshots.SourceSnapshots, Actions: actionlocal.NewResolver(nil, nil), CollectScripts: true})
	result, err := service.Workflow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	references := documentparse.References("ci.yml", content, document, nil, func(path string) string { return path })
	input, err := result.AnalyzerInput("ci.yml", "ci.yml", content, document, references)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Scripts) != 2 {
		t.Fatalf("script count = %d, want 2", len(input.Scripts))
	}
	steps := document.Workflow.Jobs["test"].Steps
	for i, step := range steps {
		want, err := snapshot.StepReference(step)
		if err != nil || input.Scripts[i].Reference != want {
			t.Fatalf("script %d lost its declaration owner: got %+v, want %+v: %v", i, input.Scripts[i].Reference, want, err)
		}
		if indexed := references["ci.yml"].Steps[step]; indexed != want {
			t.Fatalf("source index drifted from snapshot: got %+v, want %+v", indexed, want)
		}
	}
	if input.Scripts[0].Reference.Node == input.Scripts[1].Reference.Node {
		t.Fatal("aliased scalar positions collapsed separate script declarations")
	}
}

func TestMappedCompositeReferencesMatchSourceIndexes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "ci.yml")
	actionPath := filepath.Join(root, "source", "action.yml")
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/source\n      - uses: $/generated\n")
	actionContent := []byte("name: source\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - &shared\n      shell: bash\n      run: echo ok\n    - *shared\n")
	document := documentparse.Workflow(path, content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document(fsread.PhysicalPath(path), content, document)
	if err != nil {
		t.Fatal(err)
	}
	cache := actionlocal.NewCache(root, nil)
	actionCapture, err := snapshots.Sources.Capture(actionPath, actionContent, source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	cache.Configure(actionlocal.Options{Captures: snapshots.Sources.Store, Context: t.Context(), WorkingDir: root, ExplicitDocuments: map[string]struct{}{actionCapture.Revision().Document(): {}}})
	resolver := actionlocal.NewResolver(cache, map[string]string{"./generated": "./source"})
	service := New(Options{Context: t.Context(), SourceSnapshots: snapshots.SourceSnapshots, Actions: resolver, CollectScripts: true})
	result, err := service.Workflow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Batch.Scripts()) != 4 {
		t.Fatalf("script count = %d, want 4", len(result.Batch.Scripts()))
	}
	indexes := documentparse.References(path, content, document, result.Sources, fsread.PhysicalPath)
	declarations := map[source.Reference]bool{}
	for _, reference := range indexes[actionPath].Steps {
		declarations[reference] = true
	}
	for i, unit := range result.Batch.Scripts() {
		if !declarations[unit.Reference] {
			t.Fatalf("script %d drifted from source index: %+v", i, unit.Reference)
		}
		if i >= 2 {
			original := result.Batch.Scripts()[i-2]
			if unit.Reference != original.Reference || !reflect.DeepEqual(unit.Run, original.Run) {
				t.Fatal("mapped snapshot changed its declaration or script value")
			}
		}
	}
	if result.Batch.Scripts()[0].Reference == result.Batch.Scripts()[1].Reference {
		t.Fatal("aliased action declarations collapsed")
	}
	if result.Batch.Scripts()[0].Scope.Deployment.ActionPath() != filepath.Dir(actionPath) || result.Batch.Scripts()[2].Scope.Deployment.ActionPath() != "" {
		t.Fatal("source reuse contaminated per-lookup deployment paths")
	}
}

func TestScriptCollectionRejectsForeignDeclaration(t *testing.T) {
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
	indexed := documentparse.Workflow("ci.yml", content)
	foreign := documentparse.Workflow("ci.yml", content)
	analysis := compositeAnalyzer{compositionSession: &compositionSession{collectScripts: true}, sourceSnapshot: workflowSnapshot(t, content, indexed)}
	if err := analysis.collectScript(foreign.Workflow.Jobs["test"].Steps[0], nil, nil, nil); err == nil {
		t.Fatal("accepted independently decoded declaration")
	}
	if len(analysis.scripts) != 0 {
		t.Fatal("foreign declaration produced a script fact")
	}
	analysis.sourceSnapshot = nil
	if err := analysis.collectScript(&workflowlang.Step{Exec: &workflowlang.ExecRun{Run: &workflowlang.String{Value: "echo missing"}}}, nil, nil, nil); err == nil {
		t.Fatal("accepted a script without a source snapshot")
	}
}
