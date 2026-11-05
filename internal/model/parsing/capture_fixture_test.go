package parsing

import (
	"errors"
	"os"
	"slices"
	"testing"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func snapshotWorkflow(t testing.TB, snapshot SourceSnapshot) workflowdecl.WorkflowView {
	t.Helper()
	workflow, present := snapshot.WorkflowView()
	if !present {
		t.Fatal("workflow declarations unavailable")
	}
	return workflow
}

func snapshotJobSteps(t testing.TB, snapshot SourceSnapshot, key string) []workflowdecl.StepView {
	t.Helper()
	job, present := snapshotWorkflow(t, snapshot).JobsAt(key)
	if !present {
		t.Fatalf("workflow job %q unavailable", key)
	}
	return slices.Collect(job.Steps())
}

func snapshotActionSteps(t testing.TB, snapshot SourceSnapshot) []workflowdecl.StepView {
	t.Helper()
	action, present := snapshot.ActionView()
	if !present {
		t.Fatal("action declarations unavailable")
	}
	return slices.Collect(action.Steps())
}

// sourceFixtureBase explicitly retains the working directory used by these
// legacy relative-path fixtures; production receives its session base instead.
func sourceFixtureBase(t testing.TB) string {
	t.Helper()
	base, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return base
}

type capturedSnapshots struct {
	*SourceSnapshots
	store   *capture.Store
	sources *testutil.CapturedSources
}

func newCapturedSnapshots(t testing.TB, base string) *capturedSnapshots {
	t.Helper()
	store, sources := testutil.NewCapturedSources(t, base)
	registry, err := NewSourceSnapshots(store)
	if err != nil {
		t.Fatal(err)
	}
	return &capturedSnapshots{registry, store, sources}
}

func (s *capturedSnapshots) Document(path string, content []byte, document Document) (SourceSnapshot, error) {
	if path == "" {
		return SourceSnapshot{}, errors.New("fixture source identity is empty")
	}
	captured, err := s.sources.Capture(path, content, source.YAML)
	if err != nil {
		return SourceSnapshot{}, err
	}
	return s.SourceSnapshots.Document(captured, document)
}

func (s *capturedSnapshots) Action(path string, metadata *actionmeta.Metadata) (SourceSnapshot, error) {
	if path == "" || metadata == nil {
		return SourceSnapshot{}, errors.New("fixture action source is unavailable")
	}
	text, parsed := metadata.OriginalSource()
	if !parsed {
		return SourceSnapshot{}, errors.New("fixture action has no parse provenance")
	}
	captured, err := s.sources.Capture(path, []byte(text), source.YAML)
	if err != nil {
		return SourceSnapshot{}, err
	}
	return s.SourceSnapshots.Action(captured, metadata)
}

func (s *capturedSnapshots) LookupDocument(path string) (SourceSnapshot, bool) {
	captured, _, err := s.store.Lookup(s.sources.Path(path))
	if err != nil || !captured.Valid() {
		return SourceSnapshot{}, false
	}
	return s.SourceSnapshots.LookupDocument(captured.Revision().Document())
}
