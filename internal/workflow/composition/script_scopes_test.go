package composition

import (
	"slices"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func TestPreparedScriptsRetainIndependentShells(t *testing.T) {
	source := `on: push
defaults: {run: {shell: python}}
jobs:
  first:
    runs-on: ubuntu-latest
    defaults: {run: {shell: bash}}
    steps:
      - run: echo first
      - run: print('explicit')
        shell: python3
  second:
    runs-on: ubuntu-latest
    steps:
      - run: print('inherited')
`
	content := []byte(source)
	document := documentparse.Workflow("fixture.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	workflow := document.Workflow
	snapshot := workflowSnapshot(t, content, document)
	collector := &compositeAnalyzer{collectScripts: true, actions: actionlocal.NewResolver(nil, nil)}
	startWorkflowEntry(t, collector, snapshot)
	if err := collector.beginWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		job := workflow.Jobs[id]
		if err := collector.beginJob(job); err != nil {
			t.Fatal(err)
		}
		for _, step := range job.Steps {
			if err := collector.collectScript(step, nil, collector.jobScriptShell, collector.workflowScriptShell); err != nil {
				t.Fatal(err)
			}
		}
	}
	batch := prepareCollectedScripts(t, collector)
	var shells []string
	for _, script := range batch.Scripts() {
		if script.Shell != nil {
			shells = append(shells, *script.Shell)
		} else {
			shells = append(shells, "")
		}
	}
	if !slices.Equal(shells, []string{"bash", "python3", "python"}) {
		t.Fatal(shells)
	}
	// Composite declarations receive explicit shells from the indexed AST.
	meta, err := actionmeta.Parse("action.yml", []byte("name: action\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo composite\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	actionSnapshot, err := snapshots.Action("action.yml", meta)
	if err != nil {
		t.Fatal(err)
	}
	workflowGraph, err := invocation.NewBuilder(invocation.EntryAction, actionSnapshot.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := scriptexecution.NewDeployment(actionSnapshot, meta.Path(), meta.SourceMapped)
	if err != nil {
		t.Fatal(err)
	}
	actionCollector := &compositeAnalyzer{collectScripts: true, resolveScriptExecution: true, sourceSnapshots: snapshots.SourceSnapshots, sourceSnapshot: actionSnapshot, invocations: workflowGraph, context: workflowGraph.RootContext()}
	paths := execution.Paths{}
	execution.ActionOrigin(&paths, workflowGraph.RootContext(), deployment.ActionPath())
	actionCollector.scriptScope = &scriptexecution.Scope{Paths: paths, Context: workflowGraph.RootContext(), Deployment: deployment, Composite: true}
	actionCollector.scriptScopes = []*scriptexecution.Scope{actionCollector.scriptScope}
	actionSteps, ok := actionSnapshot.ActionSteps()
	if !ok || len(actionSteps) != 1 {
		t.Fatal("composite declarations unavailable")
	}
	if err := actionCollector.collectScript(actionSteps[0], nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	actionBatch := prepareCollectedScripts(t, actionCollector)
	if got := actionBatch.Scripts()[0].Shell; got == nil || *got != "bash" {
		t.Fatalf("composite explicit shell was not bound: %v", got)
	}
}
