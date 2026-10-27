package documentparse

import (
	"slices"
	"sync"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/source"
)

func TestOwnedAdmissionRejectsUnrelatedBytesAndHandBuiltDocument(t *testing.T) {
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo original}]}}\n")
	document := Workflow("ci.yml", content)
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	if _, err := snapshots.Document("ci.yml", []byte("on: pull_request\n"), document); err == nil {
		t.Fatal("accepted bytes unrelated to parsed document")
	}
	if _, err := snapshots.Document("ci.yml", content, Document{Workflow: document.Workflow}); err == nil {
		t.Fatal("accepted hand-built document without parse provenance")
	}
}

func TestOwnedDeclarationSurvivesDecoderAndPositionMutation(t *testing.T) {
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - id: original\n        name: Original\n        run: echo original\n")
	for _, access := range []string{"before binding", "position accessor", "original model after binding"} {
		t.Run(access, func(t *testing.T) {
			document := Workflow("ci.yml", content)
			cursor := document.Workflow.Jobs["build"].Steps[0]
			snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			mutate := func(step *workflowlang.Step) {
				step.ID.Value, step.Name.Value, step.Pos.Line = "changed", "Changed", 999
				switch run := step.Exec.(type) {
				case *workflowlang.ExecRun:
					run.Run.Value = "echo changed"
					run.Source.Value = "changed source mapping"
					run.Source.Spans[0].YamlLine = 999
				default:
					t.Fatal("fixture run unavailable")
				}
			}
			if access == "before binding" {
				mutate(cursor)
			}
			snapshot, err := snapshots.Document("ci.yml", content, document)
			if err != nil {
				t.Fatal(err)
			}
			view := snapshotJobSteps(t, snapshot, "build")[0]
			reference := view.Reference()
			switch access {
			case "position accessor":
				position, _ := view.Pos()
				position.Line = 999
				if fresh, _ := view.Pos(); fresh.Line == position.Line {
					t.Fatal("position accessor exposed mutable declaration state")
				}
			case "original model after binding":
				mutate(cursor)
			}
			declaration, ok := snapshots.Declaration(reference)
			if !ok || declaration != view || !snapshots.Owns(view) {
				t.Fatal("canonical handle identity changed")
			}
			position, present := declaration.Pos()
			run, isRun := declaration.Run()
			mapped, mappedOK := run.Source().Position(1, 1)
			if declaration.ID().Value() != "original" || declaration.Name().Value() != "Original" || !present || position.Line == 999 || !isRun || run.Run().Value() != "echo original" || run.Source().Value() != "echo original" || !mappedOK || mapped.Line == 999 {
				t.Fatal("legacy mutation changed frozen declaration or scalar mapping")
			}
			if declaration.Reference() != reference || declaration.SourceReference() != snapshot.Reference() || declaration.DeclaringJob() != "Build" || declaration.ContentText() != string(content) {
				t.Fatal("canonical source identity changed")
			}
		})
	}
}

func TestOwnedActionUsesPrivateOriginalText(t *testing.T) {
	content := []byte("name: original\ndescription: test\nruns:\n  using: composite\n  steps:\n    - &shared\n      shell: bash\n      run: echo original\n    - *shared\n")
	metadata, err := actionmeta.Parse("action.yml", content)
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
	detached, present := steps[0].Pos()
	if !present {
		t.Fatal("action step position unavailable")
	}
	detached.Line = 999
	if fresh, _ := steps[0].Pos(); fresh.Line == detached.Line {
		t.Fatal("action position accessor exposed mutable declaration state")
	}
	for _, step := range steps {
		run, ok := step.Run()
		position, hasPosition := step.Pos()
		if !ok || run.Run().Value() != "echo original" || !hasPosition || position.Line == 999 || step.ContentText() != original {
			t.Fatal("mutable action metadata or traversal escaped into canonical steps")
		}
	}
}

func TestOwnedIndexSurvivesAnonymousStepMutation(t *testing.T) {
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n")
	document := Workflow("ci.yml", content)
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("ci.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	cursor := document.Workflow.Jobs["build"].Steps[0]
	view := snapshotJobSteps(t, snapshot, "build")[0]
	switch action := cursor.Exec.(type) {
	case *workflowlang.ExecAction:
		action.Uses.Value = "evil/changed@v1"
	default:
		t.Fatal("fixture action absent")
	}
	refs := snapshot.CanonicalReferences()
	_, dependencies := snapshot.Declarations()
	if len(refs.Steps) != 1 || refs.Steps[0] != view.Reference() || len(dependencies) != 1 || dependencies[0].Reference != view.Reference() || dependencies[0].Uses != "actions/checkout@v4" {
		t.Fatal("final declaration projection reindexed mutable syntax")
	}
	refs.Steps[0] = source.Reference{}
	if snapshot.CanonicalReferences().Steps[0] != view.Reference() {
		t.Fatal("consumer changed canonical index")
	}
}

func TestOwnedHandlesRejectIndependentDecodeAndZero(t *testing.T) {
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
	first, second := Workflow("ci.yml", content), Workflow("ci.yml", content)
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
	if snapshots.Owns(foreignStep) || snapshots.Owns(workflowdecl.StepView{}) {
		t.Fatal("foreign or zero handle accepted")
	}
	if err := bound.ValidateStep(workflowdecl.StepView{}); err == nil {
		t.Fatal("missing declaration accepted")
	}
	zero := workflowdecl.StepView{}
	if zero.Present() || zero.Reference() != (source.Reference{}) || zero.ID().Present() {
		t.Fatal("zero handle exposes declarations")
	}
}

func TestOwnedHandlesAreScopedToRegistryBinding(t *testing.T) {
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo owned}]}}\n")
	document := Workflow("ci.yml", content)
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
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo owned}]}}\n")
	document := Workflow("ci.yml", content)
	const workers = 24
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	type result struct {
		snapshot *SourceSnapshot
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
	var canonical *SourceSnapshot
	for range workers {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if canonical == nil {
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
	content := []byte("name: broken\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo missing shell\n")
	document := Action("action.yml", "action.yml", content)
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
	if !ok || !action.Present() || action.Uses().Present() {
		t.Fatal("opaque composite step changed execution variant")
	}
}

func TestOwnedAdmissionRetainsFailedAndEmptyParseRevisions(t *testing.T) {
	for _, content := range []string{"", "null\n", "[unterminated", "jobs: !!unknown {}\n"} {
		for _, action := range []bool{false, true} {
			document := Workflow("input.yml", []byte(content))
			if action {
				document = Action("input.yml", "input.yml", []byte(content))
				if document.Metadata == nil {
					t.Fatal("action parser lost recovered metadata")
				}
			}
			snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			snapshot, err := snapshots.Document("input.yml", []byte(content), document)
			if err != nil {
				t.Fatalf("admit action=%v source=%q: %v", action, content, err)
			}
			if string(snapshot.Content()) != content || snapshot.CanonicalReferences().Document != snapshot.Reference() {
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
	content := []byte("name: recovered\ndescription: test\ninputs:\n  value:\n    unexpected: tolerated metadata error\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo recovered\n")
	metadata, parseErr := actionmeta.Parse("action.yml", content)
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
	if !ok || run.Run().Value() != "echo recovered" {
		t.Fatal("recovered declaration changed execution semantics")
	}
}
