// Package declarations provides source declaration fixtures for tests.
package declarations

import (
	"errors"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/source"
)

// CapturedSnapshots builds document declarations backed by captured fixture sources.
type CapturedSnapshots struct {
	*documentparse.SourceSnapshots
	Store   *capture.Store
	Sources *capturefixture.CapturedSources
}

// NewCapturedSnapshots creates a declaration registry for fixture sources.
func NewCapturedSnapshots(t testing.TB, base string) *CapturedSnapshots {
	t.Helper()
	store, sources := capturefixture.NewCapturedSources(t, base)
	registry, err := documentparse.NewSourceSnapshots(store)
	if err != nil {
		t.Fatal(err)
	}
	return &CapturedSnapshots{registry, store, sources}
}

// Document captures and registers a document fixture.
func (s *CapturedSnapshots) Document(path string, content []byte, document documentparse.Document) (documentparse.SourceSnapshot, error) {
	if path == "" {
		return documentparse.SourceSnapshot{}, errors.New("fixture source identity is empty")
	}
	captured, err := s.Sources.Capture(path, content, source.YAML)
	if err != nil {
		return documentparse.SourceSnapshot{}, err
	}
	return s.SourceSnapshots.Document(captured, document)
}

// Action captures and registers an action metadata fixture.
func (s *CapturedSnapshots) Action(path string, metadata *actionmeta.Metadata) (documentparse.SourceSnapshot, error) {
	if path == "" || metadata == nil {
		return documentparse.SourceSnapshot{}, errors.New("fixture action source is unavailable")
	}
	text, parsed := metadata.OriginalSource()
	if !parsed {
		return documentparse.SourceSnapshot{}, errors.New("fixture action has no parse provenance")
	}
	captured, err := s.Sources.Capture(path, []byte(text), source.YAML)
	if err != nil {
		return documentparse.SourceSnapshot{}, err
	}
	return s.SourceSnapshots.Action(captured, metadata)
}

// LookupDocument accepts either an admitted identity or a fixture source path.
func (s *CapturedSnapshots) LookupDocument(path string) (documentparse.SourceSnapshot, bool) {
	if snapshot, exists := s.SourceSnapshots.LookupDocument(path); exists {
		return snapshot, true
	}
	captured, _, err := s.Store.Lookup(s.Sources.Path(path))
	if err != nil || !captured.Valid() {
		return documentparse.SourceSnapshot{}, false
	}
	return s.SourceSnapshots.LookupDocument(captured.Revision().Document())
}
