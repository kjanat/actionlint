package documentparse

import (
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/source/capture"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/source"
)

func TestDeclarationKindConflictQuarantinesBothUses(t *testing.T) {
	for _, order := range []string{"workflow-first", "action-first", "concurrent"} {
		t.Run(order, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shared.yml")
			content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\nname: shared\ndescription: shared\nruns: {using: composite, steps: [{shell: bash, run: echo ok}]}\n")
			fixtures := newCapturedSnapshots(t, filepath.Dir(path))
			captured, err := fixtures.sources.Capture(path, content, source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Dir(path) + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(path)
			aliased, err := fixtures.sources.Store.Read(t.Context(), alias, source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			captures := []capture.Snapshot{captured, aliased}
			registry := fixtures.SourceSnapshots
			documents := []Document{Workflow(path, content), Action(path, path, content)}
			if order == "action-first" {
				documents[0], documents[1] = documents[1], documents[0]
			}
			var snapshots [2]*SourceSnapshot
			var failures [2]error
			var earlierView workflowdecl.StepView
			var earlierReference source.Reference
			bind := func(index int) {
				snapshots[index], failures[index] = registry.Document(captures[index], documents[index])
			}
			if order == "concurrent" {
				var workers sync.WaitGroup
				start := make(chan struct{})
				for index := range 2 {
					workers.Go(func() { <-start; bind(index) })
				}
				close(start)
				workers.Wait()
			} else {
				bind(0)
				if failures[0] != nil {
					t.Fatal(failures[0])
				}
				references := snapshots[0].CanonicalReferences().Steps
				if len(references) == 0 {
					t.Fatal("initial declaration has no indexed step")
				}
				earlierReference = references[0]
				var present bool
				earlierView, present = registry.Declaration(earlierReference)
				if !present || !earlierView.Present() {
					t.Fatal("initial canonical declaration unavailable")
				}
				bind(1)
			}
			var previous *SourceSnapshot
			for index, failure := range failures {
				if failure == nil {
					previous = snapshots[index]
				} else if _, ok := errors.AsType[*capture.ConflictError](failure); !ok {
					t.Fatalf("kind mismatch returned untyped error: %v", failure)
				}
			}
			if previous == nil {
				t.Fatal("neither declaration was initially admitted")
			}
			for _, document := range documents {
				if _, err := registry.Document(captured, document); err == nil {
					t.Fatal("quarantined declaration was readmitted")
				} else if conflict, ok := errors.AsType[*capture.ConflictError](err); !ok || conflict.Error() != "source-revision-conflict: "+strconv.Quote(captured.Revision().Document())+": conflicting declaration kinds: action, workflow" {
					t.Fatalf("non-deterministic kind conflict: %v", err)
				}
			}
			if _, ok := registry.Lookup(previous.Reference()); ok {
				t.Fatal("registry returned the initially admitted declaration")
			}
			if _, ok := registry.LookupDocument(captured.Revision().Document()); ok {
				t.Fatal("registry retained a live document after conflict")
			}
			if earlierView.Present() {
				if _, ok := registry.Declaration(earlierReference); ok || registry.Owns(earlierView) {
					t.Fatal("quarantined view retained semantic ownership")
				}
				if err := previous.ValidateStep(earlierView); err == nil {
					t.Fatal("quarantined step reference remained available")
				}
			}
			if _, ok := previous.WorkflowView(); ok {
				t.Fatal("quarantined workflow remained available")
			}
			if _, ok := previous.ActionView(); ok {
				t.Fatal("quarantined action remained available")
			}
			if previous.Kind() != KindUnknown || string(previous.Content()) != string(content) {
				t.Fatal("quarantine changed bytes or exposed a semantic kind")
			}
			if err := fixtures.sources.Store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := fixtures.sources.Store.Sources()
			if err != nil || len(rows) != 1 || rows[0].State != capture.Conflicted || rows[0].Revision.Valid() || rows[0].Code != "source-revision-conflict" || len(rows[0].Aliases) != 2 {
				t.Fatalf("published conflicted declaration: rows=%+v error=%v", rows, err)
			}
		})
	}
}
