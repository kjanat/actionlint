// Package declarations provides source declaration fixtures for tests.
package declarations

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

// CapturedSnapshots captures fixture sources and admits them into one
// declaration registry. The registry and fixture sources share the store
// through private fields, so Document always captures into the store the
// registry uses for verification.
type CapturedSnapshots struct {
	registry *parsing.SourceSnapshots
	store    *capture.Store
	sources  *capturefixture.CapturedSources
}

// NewCapturedSnapshots returns the declaration registry and capture store to
// hand to code under test, and the fixture that admits documents into them.
// The store is closed when the test ends.
func NewCapturedSnapshots(tb testing.TB, base string) (*parsing.SourceSnapshots, *capture.Store, *CapturedSnapshots) {
	tb.Helper()
	store, sources := capturefixture.NewCapturedSources(tb, base)
	registry, err := parsing.NewSourceSnapshots(store)
	if err != nil {
		tb.Fatal(err)
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
func (s *CapturedSnapshots) Document(path string, content []byte, document parsing.Document) (parsing.SourceSnapshot, error) {
	if path == "" {
		return parsing.SourceSnapshot{}, errors.New("fixture source identity is empty")
	}
	captured, err := s.sources.Capture(path, content, source.YAML)
	if err != nil {
		return parsing.SourceSnapshot{}, err
	}
	return s.registry.Document(captured, document)
}

// Action captures and registers an action metadata fixture.
func (s *CapturedSnapshots) Action(path string, metadata *actionmeta.Metadata) (parsing.SourceSnapshot, error) {
	if path == "" || metadata == nil {
		return parsing.SourceSnapshot{}, errors.New("fixture action source is unavailable")
	}
	text, parsed := metadata.OriginalSource()
	if !parsed {
		return parsing.SourceSnapshot{}, errors.New("fixture action has no parse provenance")
	}
	captured, err := s.sources.Capture(path, []byte(text), source.YAML)
	if err != nil {
		return parsing.SourceSnapshot{}, err
	}
	return s.registry.Action(captured, metadata)
}

// LookupDocument accepts either an admitted identity or a fixture source path.
func (s *CapturedSnapshots) LookupDocument(path string) (parsing.SourceSnapshot, bool) {
	if snapshot, exists := s.registry.LookupDocument(path); exists {
		return snapshot, true
	}
	captured, _, err := s.store.Lookup(s.sources.Path(path))
	if err != nil || !captured.Valid() {
		return parsing.SourceSnapshot{}, false
	}
	return s.registry.LookupDocument(captured.Revision().Document())
}
