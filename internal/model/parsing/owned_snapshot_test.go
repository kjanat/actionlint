package parsing

import (
	"slices"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

func TestOwnedAdmissionRejectsUnrelatedBytesAndHandBuiltDocument(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo original}]}}\n")
	document := Workflow(content)
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	if _, err := snapshots.Document("ci.yml", []byte("on: pull_request\n"), document); err == nil {
		t.Fatal("accepted bytes unrelated to parsed document")
	}
	if _, err := snapshots.Document("ci.yml", content, Document{Workflow: document.Workflow}); err == nil {
		t.Fatal("accepted hand-built document without parse provenance")
	}
}

func TestOwnedActionUsesPrivateOriginalText(t *testing.T) {
	t.Parallel()
	content := []byte("name: original\ndescription: test\nruns:\n  using: composite\n  steps:\n    - &shared\n      shell: bash\n      run: echo original\n    - *shared\n")
	metadata, err := actionmeta.ParseActionMetadata("action.yml", content)
	if err != nil {
		t.Fatal(err)
	}
	original := string(content)
	content[0] = '!'
	metadata.Source[0] = '?'
	metadata.Root.Content = nil
	metadata.Runs.Steps = nil
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Action("action.yml", metadata)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := snapshot.ActionView()
	if !ok {
		t.Fatal("action body absent after metadata mutation")
	}
	steps := slices.Collect(body.Steps())
	if len(steps) != 2 || steps[0].Reference() == steps[1].Reference() {
		t.Fatal("action alias sites collapsed")
	}
	detached, present := steps[0].Position()
	if !present {
		t.Fatal("action step position unavailable")
	}
	detached.Line = 999
	if fresh, _ := steps[0].Position(); fresh.Line == detached.Line {
		t.Fatal("action position accessor exposed mutable declaration state")
	}
	for _, step := range steps {
		run, ok := step.Run()
		position, hasPosition := step.Position()
		if !ok || run.Run.Text() != "echo original" || !hasPosition || position.Line == 999 || step.ContentText() != original {
			t.Fatal("mutable action metadata or traversal escaped into canonical steps")
		}
	}
}

func TestOwnedIndexReturnsCopiesForAnonymousStep(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n")
	document := Workflow(content)
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	view := snapshotJobSteps(t, snapshot, "build")[0]
	refs := snapshot.BoundDocument().SourceIndex()
	_, dependencies := snapshot.BoundDocument().Declarations()
	if len(refs.Steps) != 1 || refs.Steps[0] != view.Reference() || len(dependencies) != 1 || dependencies[0].Reference != view.Reference() || dependencies[0].Uses != "actions/checkout@v4" {
		t.Fatal("final declaration projection lost the anonymous step")
	}
	refs.Steps[0] = sourcetext.Reference{}
	if snapshot.BoundDocument().SourceIndex().Steps[0] != view.Reference() {
		t.Fatal("consumer changed canonical index")
	}
}

func TestOwnedHandlesRejectIndependentDecodeAndZero(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
	first, second := Workflow(content), Workflow(content)
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	other := newCapturedSnapshots(t, sourceFixtureBase(t))
	bound, err := snapshots.Document("ci.yml", content, first)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := snapshots.Document("ci.yml", content, second)
	if err != nil || reused != bound {
		t.Fatal("same revision did not retain original parse record")
	}
	foreign, err := other.Document("ci.yml", content, second)
	if err != nil {
		t.Fatal(err)
	}
	foreignStep := snapshotJobSteps(t, foreign, "build")[0]
	if err := reused.ValidateStep(foreignStep); err == nil {
		t.Fatal("foreign decode accepted as canonical declaration")
	}
	if snapshots.Owns(foreignStep) || snapshots.Owns(workflows.BoundStep{}) {
		t.Fatal("foreign or zero handle accepted")
	}
	if err := bound.ValidateStep(workflows.BoundStep{}); err == nil {
		t.Fatal("missing declaration accepted")
	}
	zero := workflows.BoundStep{}
	if zero.Present() || zero.Reference() != (sourcetext.Reference{}) {
		t.Fatal("zero handle exposes declarations")
	}
}

func TestOwnedHandlesAreScopedToRegistryBinding(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo owned}]}}\n")
	document := Workflow(content)
	firstRegistry := newCapturedSnapshots(t, sourceFixtureBase(t))
	secondRegistry := newCapturedSnapshots(t, sourceFixtureBase(t))
	first, err := firstRegistry.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondRegistry.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	firstCursor := snapshotJobSteps(t, first, "build")[0]
	secondCursor := snapshotJobSteps(t, second, "build")[0]
	firstCanonical, firstOK := firstRegistry.Declaration(firstCursor.Reference())
	secondCanonical, secondOK := secondRegistry.Declaration(secondCursor.Reference())
	if !firstOK || !secondOK || firstCanonical != firstCursor || secondCanonical != secondCursor {
		t.Fatal("registry declaration lookup did not return its canonical handle")
	}
	if !firstRegistry.Owns(firstCursor) || !secondRegistry.Owns(secondCursor) {
		t.Fatal("registry rejected its own canonical handle")
	}
	if firstCursor == secondCursor || firstRegistry.Owns(secondCursor) || secondRegistry.Owns(firstCursor) {
		t.Fatal("registry accepted another binding's handle for the same record and identity")
	}
}

func TestConcurrentRegistryBindingSharesCanonicalBoundHandle(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo owned}]}}\n")
	document := Workflow(content)
	const workers = 24
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	type result struct {
		snapshot SourceSnapshot
		err      error
	}
	results := make(chan result, workers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(workers)
	for range workers {
		go func() {
			ready.Done()
			<-start
			snapshot, err := snapshots.Document("ci.yml", content, document)
			results <- result{snapshot: snapshot, err: err}
		}()
	}
	ready.Wait()
	close(start)
	var canonical SourceSnapshot
	for range workers {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !canonical.Present() {
			canonical = result.snapshot
		} else if canonical != result.snapshot {
			t.Fatal("concurrent bind returned a noncanonical snapshot")
		}
	}
	view := snapshotJobSteps(t, canonical, "build")[0]
	declaration, ok := snapshots.Declaration(view.Reference())
	if !ok || declaration != view || !snapshots.Owns(view) {
		t.Fatal("concurrent bind did not preserve canonical bound handle identity")
	}
}

func TestOwnedOpaqueCompositeStepRemainsInvalidActionCall(t *testing.T) {
	t.Parallel()
	content := []byte("name: broken\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo missing shell\n")
	document := Action("action.yml", content)
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("action.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := snapshot.ActionView()
	if !ok {
		t.Fatal("recovered action body unavailable")
	}
	steps := slices.Collect(body.Steps())
	if len(steps) != 1 {
		t.Fatal("recovered step missing")
	}
	action, ok := steps[0].Action()
	if !ok || action == nil || action.Uses != nil {
		t.Fatal("opaque composite step changed execution variant")
	}
}

func TestOwnedAdmissionRetainsFailedAndEmptyParseRevisions(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"", "null\n", "[unterminated", "jobs: !!unknown {}\n"} {
		for _, action := range []bool{false, true} {
			document := Workflow([]byte(content))
			if action {
				document = Action("input.yml", []byte(content))
				if document.Metadata == nil {
					t.Fatal("action parser lost recovered metadata")
				}
			}
			snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			snapshot, err := snapshots.Document("input.yml", []byte(content), document)
			if err != nil {
				t.Fatalf("admit action=%v source=%q: %v", action, content, err)
			}
			if string(snapshot.Content()) != content || snapshot.BoundDocument().SourceIndex().Document != snapshot.Reference() {
				t.Fatal("failed parse lost original source revision")
			}
			found, ok := snapshots.LookupDocument("./input.yml")
			if !ok || found != snapshot {
				t.Fatal("document lookup lost canonical revision")
			}
		}
	}
}

func TestOwnedActionAdmissionRetainsRecoveredDeclarations(t *testing.T) {
	t.Parallel()
	content := []byte("name: recovered\ndescription: test\ninputs:\n  value:\n    required: [invalid]\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo recovered\n")
	metadata, parseErr := actionmeta.ParseActionMetadata("action.yml", content)
	if parseErr == nil || len(metadata.Runs.Steps) != 1 {
		t.Fatal("fixture must recover a declaration alongside a metadata error")
	}
	metadata.Runs.Steps = nil
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Action("action.yml", metadata)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := snapshot.ActionView()
	if !ok {
		t.Fatal("recovered action body missing")
	}
	steps := slices.Collect(body.Steps())
	if len(steps) != 1 {
		t.Fatal("recovered declaration lost during admission")
	}
	run, ok := steps[0].Run()
	if !ok || run.Run.Text() != "echo recovered" {
		t.Fatal("recovered declaration changed execution semantics")
	}
}
