package composition

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func TestWorkflowUsesSnapshotRegistryWhenOptionIsNil(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}]}}\n", map[string]string{
		"child/action.yml": "name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n",
	}, nil)
	registry := f.root.Registry()
	f.service.options.SourceSnapshots = nil
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Batch.Scripts()) != 1 {
		t.Fatalf("prepared scripts = %d, want 1", len(result.Batch.Scripts()))
	}
	declaration, exists := registry.Step(result.Batch.Scripts()[0].Reference)
	if !exists || declaration.Source == nil || declaration.Step == nil {
		t.Fatal("nested action declaration was not added to the workflow snapshot registry")
	}
}

func TestWorkflowRejectsDifferentSnapshotRegistry(t *testing.T) {
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
	firstDocument := documentparse.Workflow("ci.yml", content)
	secondDocument := documentparse.Workflow("ci.yml", content)
	var firstRegistry, secondRegistry documentparse.SourceSnapshots
	if _, err := firstRegistry.Document("ci.yml", content, firstDocument); err != nil {
		t.Fatal(err)
	}
	secondSnapshot, err := secondRegistry.Document("ci.yml", content, secondDocument)
	if err != nil {
		t.Fatal(err)
	}
	service := New(Options{Context: t.Context(), SourceSnapshots: &firstRegistry, Actions: actionlocal.NewResolver(nil, nil)})
	if _, err := service.Workflow(secondSnapshot); err == nil || !strings.Contains(err.Error(), "different registry") {
		t.Fatalf("different source registry was accepted: %v", err)
	}
}

func TestPreparedNestedScriptsUseDeclaringJobAndDeploymentPath(t *testing.T) {
	workflow := "on: push\njobs:\n  Release:\n    runs-on: ubuntu-latest\n    steps: [{uses: $/outer}]\n  Verify:\n    runs-on: ubuntu-latest\n    steps: [{uses: $/outer}]\n"
	outerSource := "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n"
	innerSource := "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n"
	f := newInvocationFixture(t, t.Context(), workflow, map[string]string{"outer/action.yml": outerSource, "inner/action.yml": innerSource}, nil)
	outerConfig, innerConfig := &repoconfig.Config{}, &repoconfig.Config{}
	outerPath := filepath.Join(f.service.options.Paths.Workspace, "outer", "action.yml")
	innerPath := filepath.Join(f.service.options.Paths.Workspace, "inner", "action.yml")
	f.service.options.ConfigForFile = func(path string) (*repoconfig.Config, error) {
		if path == outerPath {
			return outerConfig, nil
		}
		return innerConfig, nil
	}
	lookup, _ := f.cache.LookupMetadata("$/inner")
	metadata := lookup.Metadata()
	if metadata == nil {
		t.Fatal("fixture inner metadata absent")
	}
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	units := result.Batch.Scripts()
	if len(units) != 2 || units[0].Reference != units[1].Reference || units[0].Owner == nil || units[1].Owner == nil || units[0].Owner == units[1].Owner {
		t.Fatalf("depth-two invocations did not retain one declaration with distinct owners: %+v", units)
	}
	wantJobs := map[string]bool{"Release": false, "Verify": false}
	for i, unit := range units {
		fact, exists := result.Batch.Fact(i, false)
		_, expectedJob := wantJobs[unit.Origin.Job]
		graphScript, existsInGraph := result.Batch.Invocations().Script(unit.Occurrence)
		path, hasPath := result.Batch.Invocations().Path(graphScript.Owner)
		if !exists || !existsInGraph || !hasPath || len(path) != 2 || fact.JobID != unit.Origin.Job || unit.Owner.Job != unit.Origin.Job || !expectedJob {
			t.Fatalf("script %d lost source-derived original job ID: unit=%+v fact=%+v", i, unit, fact)
		}
		wantJobs[unit.Origin.Job] = true
		if unit.Path != metadata.Path() || unit.Path != innerPath || string(result.Sources[unit.Path]) != innerSource || result.Configs[unit.Path] != innerConfig || unit.Config != innerConfig || unit.Scope.Config != innerConfig {
			t.Fatalf("script %d lost deployment display path, source, or config: path=%q metadata=%q sources=%v configs=%v", i, unit.Path, metadata.Path(), result.Sources, result.Configs)
		}
	}
	if result.Configs[outerPath] != outerConfig {
		t.Fatalf("outer invocation config was not retained: %+v", result.Configs)
	}
	if !wantJobs["Release"] || !wantJobs["Verify"] {
		t.Fatalf("prepared jobs did not preserve both original identifiers: %+v", wantJobs)
	}
}

func TestStandaloneNestedScriptHasNoJobIdentity(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo unused}]}}\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n",
	}, nil)
	lookup, _ := f.cache.LookupMetadata("$/outer")
	result, err := standaloneActionForTest(t, f.service, lookup.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	units := result.Batch.Scripts()
	if len(units) != 1 {
		t.Fatalf("standalone nested scripts = %d, want 1", len(units))
	}
	fact, exists := result.Batch.Fact(0, false)
	if !exists || units[0].Origin.Job != "" || units[0].Owner.Job != "" || fact.JobID != "" {
		t.Fatalf("standalone nested script fabricated a job identity: unit=%+v fact=%+v", units[0], fact)
	}
}

func TestNestedPreparedJobIgnoresCorruptedCollectorState(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {Build: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n",
	}, nil)
	workflow, available := f.root.Workflow()
	if !available {
		t.Fatal("fixture workflow unavailable")
	}
	job := workflow.Jobs["build"]
	analysis := &compositeAnalyzer{
		ctx: t.Context(), actions: f.service.options.Actions, sourceSnapshots: f.snapshots,
		collectScripts: true,
	}
	analysis.budget = invocation.DefaultBudget()
	if err := analysis.startEntry(invocation.EntryWorkflow, f.root); err != nil {
		t.Fatal(err)
	}
	if err := analysis.beginWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	if err := analysis.beginJob(job); err != nil {
		t.Fatal(err)
	}
	call := job.Steps[0]
	reference, err := f.root.StepReference(call)
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.root.StepView(call)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := execution.CallFacts(view, analysis.actions.Platform())
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := analysis.invocations.Begin(analysis.invocations.RootContext(), reference, facts)
	if err != nil {
		t.Fatal(err)
	}
	callContext, admitted := ticket.Context()
	if !admitted {
		t.Fatal("outer action call was not admitted")
	}
	lookup, _ := f.cache.LookupMetadata("$/outer")
	meta := lookup.Metadata()
	if meta == nil {
		t.Fatal("outer action metadata unavailable")
	}
	snapshot, err := analysis.sourceSnapshots.Action(fsread.PhysicalPath(meta.Path()), meta)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := invocation.Expanded(snapshot.Reference())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := analysis.invocations.Resolve(callContext, outcome); err != nil {
		t.Fatal(err)
	}
	analysis.context, analysis.sourceSnapshot = callContext, snapshot
	analysis.jobID = "corrupted nested collector state"
	if err := analysis.visitMetadataScripts(meta, callContext, nil); err != nil {
		t.Fatal(err)
	}
	if err := analysis.invocations.Close(callContext); err != nil {
		t.Fatal(err)
	}
	analysis.context = analysis.invocations.RootContext()
	batch := prepareCollectedScripts(t, analysis)
	unit := batch.Scripts()[0]
	fact, exists := batch.Fact(0, false)
	if !exists || unit.Origin.Job != "Build" || unit.Owner.Job != "Build" || fact.JobID != "Build" {
		t.Fatalf("nested prepared output trusted mutable collector job state: unit=%+v fact=%+v", unit, fact)
	}
}

func TestStandaloneDeploymentFailureLeavesNoScope(t *testing.T) {
	meta, err := actionmeta.Parse("source/action.yml", []byte("name: action\ndescription: test\nruns: {using: composite, steps: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var registry documentparse.SourceSnapshots
	snapshot, err := registry.Action(fsread.PhysicalPath(meta.Path()), meta)
	if err != nil {
		t.Fatal(err)
	}
	analysis := &compositeAnalyzer{sourceSnapshots: &registry}
	analysis.budget = invocation.DefaultBudget()
	if err := analysis.startEntry(invocation.EntryAction, snapshot); err != nil {
		t.Fatal(err)
	}
	foreign := *meta
	foreign.Directory = "foreign"
	deployment, err := scriptexecution.NewDeployment(snapshot, meta.Path(), meta.SourceMapped)
	if err != nil {
		t.Fatal(err)
	}
	originalErr := visitStandaloneAction(&foreign, deployment, nil, analysis)
	if originalErr == nil || len(analysis.scriptScopes) != 0 {
		t.Fatalf("failed deployment mutated scope collection: error=%v scopes=%+v", originalErr, analysis.scriptScopes)
	}
	retainedErr := analysis.interrupt(originalErr)
	graph, err := analysis.invocations.Publish()
	if err != nil {
		t.Fatal(err)
	}
	batch, batchErr := scriptexecution.NewBatch(graph, scriptexecution.Collection{Declarations: &registry, Scopes: analysis.scriptScopes})
	if !errors.Is(retainedErr, scriptexecution.ErrInvalidBatch) || batchErr != nil || !batch.Valid() || len(batch.Scopes()) != 0 {
		t.Fatalf("deployment failure was lost or produced an invalid interrupted batch: original=%v retained=%v batch=%v scopes=%+v", originalErr, retainedErr, batchErr, batch.Scopes())
	}
}

func TestPreparedRootJobComesFromSnapshotNotCollectorState(t *testing.T) {
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
	document := documentparse.Workflow("ci.yml", content)
	var registry documentparse.SourceSnapshots
	snapshot, err := registry.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := snapshot.Workflow()
	if !ok {
		t.Fatal("workflow snapshot unavailable")
	}
	analysis := &compositeAnalyzer{collectScripts: true, sourceSnapshot: snapshot, sourceSnapshots: &registry}
	analysis.budget = invocation.DefaultBudget()
	if err := analysis.startEntry(invocation.EntryWorkflow, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := analysis.beginWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	analysis.jobID = "corrupted collector value"
	if err := analysis.collectScript(workflow.Jobs["build"].Steps[0], nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	batch := prepareCollectedScripts(t, analysis)
	unit := batch.Scripts()[0]
	fact, exists := batch.Fact(0, false)
	if !exists || unit.Origin.Job != "Build" || fact.JobID != "Build" {
		t.Fatalf("prepared output trusted mutable collector job state: unit=%+v fact=%+v", unit, fact)
	}
	if unit.Reference == (source.Reference{}) {
		t.Fatal("prepared root script lost its source reference")
	}
}
