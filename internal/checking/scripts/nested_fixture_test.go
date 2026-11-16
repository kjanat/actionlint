package scripts

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowexecution "actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func nestedActionBatch(t *testing.T, collection Collection, snapshot parsing.SourceSnapshot, deployments ...Deployment) (invocation.Graph, Collection) {
	t.Helper()
	if len(deployments) == 0 {
		deployments = []Deployment{collection.Scopes[0].Deployment}
	}
	path := filepath.Join(filepath.Dir(snapshot.Reference().Document), "workflow.yml")
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n" + strings.Repeat("      - uses: $/source\n", len(deployments)))
	document := parsing.Workflow(content)
	registry, _, declarations := newCapturedSnapshots(t, filepath.Dir(path))
	actionPath := snapshot.Capture().Aliases()[0]
	actionContent := snapshot.Content()
	var err error
	snapshot, err = declarations.Document(actionPath, actionContent, parsing.Action(actionPath, actionContent))
	if err != nil {
		t.Fatal(err)
	}
	collection.Declarations = registry
	root, err := declarations.Document(path, content, document)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, root.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	baseScope := *collection.Scopes[0]
	collection.Scopes = []*Scope{{Context: builder.RootContext()}}
	collection.Scripts = nil
	body, _ := snapshot.ActionView()
	workflow, _ := root.WorkflowView()
	job, _ := workflow.Job("build")
	steps := slices.Collect(job.Steps())
	for i, deployment := range deployments {
		site := steps[i].Reference()
		ticket, err := builder.Begin(builder.RootContext(), site, invocation.Facts{Uses: "$/source", HasUses: true, Condition: invocation.ConditionEnabled})
		if err != nil {
			t.Fatal(err)
		}
		owner, admitted := ticket.Context()
		if !admitted {
			t.Fatal("fixture call was not admitted")
		}
		outcome, err := invocation.Expanded(snapshot.Reference())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := builder.Resolve(owner, outcome); err != nil {
			t.Fatal(err)
		}
		scope := baseScope
		scope.Context, scope.Deployment = owner, deployment
		workflowexecution.ActionOrigin(&scope.Paths, owner, deployment.ActionPath())
		collection.Scopes = append(collection.Scopes, &scope)
		for step := range body.Steps() {
			ref := step.Reference()
			id, err := builder.AddScript(owner, ref, invocation.ScriptRun, "")
			if err != nil {
				t.Fatal(err)
			}
			collection.Scripts = append(collection.Scripts, ScriptRequest{Occurrence: id})
		}
		if err := builder.Close(owner); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	return graph, collection
}
