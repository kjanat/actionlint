package parsing

import (
	"errors"
	"os"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext/capture"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

func snapshotWorkflow(tb testing.TB, snapshot SourceSnapshot) workflows.Workflow {
	tb.Helper()
	workflow, present := snapshot.WorkflowView()
	if !present {
		tb.Fatal("workflow declarations unavailable")
	}
	return workflow
}

func snapshotJobSteps(tb testing.TB, snapshot SourceSnapshot, key string) []workflows.Step {
	tb.Helper()
	job, present := snapshotWorkflow(tb, snapshot).Job(key)
	if !present {
		tb.Fatalf("workflow job %q unavailable", key)
	}
	return slices.Collect(job.Steps())
}

func snapshotActionSteps(tb testing.TB, snapshot SourceSnapshot) []workflows.Step {
	tb.Helper()
	action, present := snapshot.ActionView()
	if !present {
		tb.Fatal("action declarations unavailable")
	}
	return slices.Collect(action.Steps())
}

// sourceFixtureBase explicitly retains the working directory used by these
// legacy relative-path fixtures; production receives its session base instead.
func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	base, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	return base
}

type capturedSnapshots struct {
	*SourceSnapshots
	store   *capture.Store
	sources *capturefixture.CapturedSources
}

func newCapturedSnapshots(tb testing.TB, base string) *capturedSnapshots {
	tb.Helper()
	store, sources := capturefixture.NewCapturedSources(tb, base)
	registry, err := NewSourceSnapshots(store)
	if err != nil {
		tb.Fatal(err)
	}
	return &capturedSnapshots{registry, store, sources}
}

func (s *capturedSnapshots) Document(path string, content []byte, document Document) (SourceSnapshot, error) {
	if path == "" {
		return SourceSnapshot{}, errors.New("fixture source identity is empty")
	}
	captured, err := s.sources.Capture(path, content, sourcetext.YAML)
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
	captured, err := s.sources.Capture(path, []byte(text), sourcetext.YAML)
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
