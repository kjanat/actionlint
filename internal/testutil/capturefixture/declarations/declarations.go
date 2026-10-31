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

// CapturedSnapshots captures fixture sources and admits them into one
// declaration registry. The registry, the capture store and the fixture
// sources share that one store. The fields are private so a caller cannot
// replace one of them and make Document capture into a store the registry
// does not verify against.
type CapturedSnapshots struct {
	registry *documentparse.SourceSnapshots
	store    *capture.Store
	sources  *capturefixture.CapturedSources
}

// NewCapturedSnapshots returns the declaration registry and capture store to
// hand to code under test, and the fixture that admits documents into them.
// The store is closed when the test ends.
func NewCapturedSnapshots(t testing.TB, base string) (*documentparse.SourceSnapshots, *capture.Store, *CapturedSnapshots) {
	t.Helper()
	store, sources := capturefixture.NewCapturedSources(t, base)
	registry, err := documentparse.NewSourceSnapshots(store)
	if err != nil {
		t.Fatal(err)
	}
	return registry, store, &CapturedSnapshots{registry, store, sources}
}

// Path resolves a relative fixture path against the fixture base.
func (s *CapturedSnapshots) Path(path string) string { return s.sources.Path(path) }

// Capture stores content as the fixture bytes for path and captures it
// without admitting it into the registry.
func (s *CapturedSnapshots) Capture(path string, content []byte, coordinates source.Coordinates) (capture.Snapshot, error) {
	return s.sources.Capture(path, content, coordinates)
}

// Document captures and registers a document fixture.
func (s *CapturedSnapshots) Document(path string, content []byte, document documentparse.Document) (documentparse.SourceSnapshot, error) {
	if path == "" {
		return documentparse.SourceSnapshot{}, errors.New("fixture source identity is empty")
	}
	captured, err := s.sources.Capture(path, content, source.YAML)
	if err != nil {
		return documentparse.SourceSnapshot{}, err
	}
	return s.registry.Document(captured, document)
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
	captured, err := s.sources.Capture(path, []byte(text), source.YAML)
	if err != nil {
		return documentparse.SourceSnapshot{}, err
	}
	return s.registry.Action(captured, metadata)
}

// LookupDocument accepts either an admitted identity or a fixture source path.
func (s *CapturedSnapshots) LookupDocument(path string) (documentparse.SourceSnapshot, bool) {
	if snapshot, exists := s.registry.LookupDocument(path); exists {
		return snapshot, true
	}
	captured, _, err := s.store.Lookup(s.sources.Path(path))
	if err != nil || !captured.Valid() {
		return documentparse.SourceSnapshot{}, false
	}
	return s.registry.LookupDocument(captured.Revision().Document())
}
