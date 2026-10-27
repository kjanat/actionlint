package documentparse

import (
	"errors"
	"os"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	testutil "actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/source"
)

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
	sources *testutil.CapturedSources
}

func newCapturedSnapshots(t testing.TB, base string) *capturedSnapshots {
	t.Helper()
	sources := testutil.NewCapturedSources(t, base)
	registry, err := NewSourceSnapshots(sources.Store)
	if err != nil {
		t.Fatal(err)
	}
	return &capturedSnapshots{registry, sources}
}

func (s *capturedSnapshots) Document(path string, content []byte, document Document) (*SourceSnapshot, error) {
	if path == "" {
		return nil, errors.New("fixture source identity is empty")
	}
	captured, err := s.sources.Capture(path, content, source.YAML)
	if err != nil {
		return nil, err
	}
	return s.SourceSnapshots.Document(captured, document)
}

func (s *capturedSnapshots) Action(path string, metadata *actionmeta.Metadata) (*SourceSnapshot, error) {
	if path == "" || metadata == nil {
		return nil, errors.New("fixture action source is unavailable")
	}
	text, parsed := metadata.OriginalSource()
	if !parsed {
		return nil, errors.New("fixture action has no parse provenance")
	}
	captured, err := s.sources.Capture(path, []byte(text), source.YAML)
	if err != nil {
		return nil, err
	}
	return s.SourceSnapshots.Action(captured, metadata)
}

func (s *capturedSnapshots) LookupDocument(path string) (*SourceSnapshot, bool) {
	captured, _, err := s.sources.Store.Lookup(s.sources.Path(path))
	if err != nil || !captured.Valid() {
		return nil, false
	}
	return s.SourceSnapshots.LookupDocument(captured.Revision().Document())
}
