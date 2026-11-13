package composites

import (
	"errors"
	"testing"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

type preparationObserver struct {
	workflowvisit.Base
	t              *testing.T
	prepared       map[string]bool
	entryDocument  string
	entries        int
	metadataChecks int
}

var _ checkcalls.InvocationParticipant = (*preparationObserver)(nil)

func (r *preparationObserver) VisitWorkflowPre(workflowdecl.WorkflowView) error {
	if !r.prepared[r.entryDocument] {
		r.t.Fatal("workflow checks started before source preparation")
	}
	return nil
}

func (r *preparationObserver) EnterInvocation(call checkcalls.Invocation) error {
	if !r.prepared[call.Source.Reference().Document] {
		r.t.Fatal("composite checks started before source preparation")
	}
	r.entries++
	return nil
}

func (*preparationObserver) LeaveInvocation(invocation.Context) error        { return nil }
func (*preparationObserver) VisitInvocationStep(workflowdecl.StepView) error { return nil }
func (*preparationObserver) VisitOpaqueAction(workflowdecl.StepView) error   { return nil }

func (r *preparationObserver) CheckStandaloneMetadata(_ *actionmeta.Metadata, snapshot parsing.SourceSnapshot, _ actionmeta.ReferencedFiles) error {
	if !r.prepared[snapshot.Reference().Document] {
		r.t.Fatal("standalone metadata checks started before source preparation")
	}
	r.metadataChecks++
	return nil
}

func (r *preparationObserver) ForMetadata(*workspaceactions.Resolver) workflowvisit.Rule { return r }

func TestSourcePreparationPrecedesStandaloneMetadataChecks(t *testing.T) {
	t.Parallel()
	metadata, err := actionmeta.Parse("action.yml", []byte("name: fixture\ndescription: fixture\nruns: {using: composite, steps: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
	prepared := map[string]bool{}
	observer := &preparationObserver{Base: workflowvisit.NewBase("preparation-observer", ""), t: t, prepared: prepared}
	options := Options{Context: t.Context(), Actions: workspaceactions.NewResolver(nil, nil), Rules: []workflowvisit.Rule{observer}, PrepareSource: func(snapshot parsing.SourceSnapshot) error {
		prepared[snapshot.Reference().Document] = true
		return nil
	}}
	if _, err := standaloneActionForTest(t, options, metadata); err != nil {
		t.Fatal(err)
	}
	if observer.metadataChecks != 1 || len(prepared) != 1 {
		t.Fatalf("metadata checks=%d prepared sources=%d", observer.metadataChecks, len(prepared))
	}
}

func TestSourcePreparationPrecedesWorkflowAndCompositeChecks(t *testing.T) {
	t.Parallel()
	fixture := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo ok}]}\n",
	}, nil)
	prepared := map[string]bool{}
	observer := &preparationObserver{Base: workflowvisit.NewBase("preparation-observer", ""), t: t, prepared: prepared, entryDocument: fixture.root.Reference().Document}
	fixture.options.PrepareSource = func(snapshot parsing.SourceSnapshot) error {
		prepared[snapshot.Reference().Document] = true
		return nil
	}
	fixture.options.Rules = append(fixture.options.Rules, observer)
	if _, err := Workflow(fixture.options, fixture.root); err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 3 || observer.entries != 2 {
		t.Fatalf("prepared %d sources and entered %d composites", len(prepared), observer.entries)
	}
}

func TestSourcePreparationFailureStopsChecks(t *testing.T) {
	t.Parallel()
	fixture := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", nil, nil)
	failure := errors.New("dependency acquisition failed")
	fixture.options.PrepareSource = func(parsing.SourceSnapshot) error { return failure }
	fixture.options.Rules = []workflowvisit.Rule{&preparationObserver{Base: workflowvisit.NewBase("preparation-observer", ""), t: t, prepared: map[string]bool{}}}
	if _, err := Workflow(fixture.options, fixture.root); !errors.Is(err, failure) {
		t.Fatalf("source preparation failure was lost: %v", err)
	}
}
