// Package declarations provides source declaration fixtures for tests.
package declarations

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

// CapturedSnapshots captures fixture sources into the store the declaration
// registry uses for verification.
type CapturedSnapshots struct {
	registry *parsing.SourceSnapshots
	store    *capture.Store
	sources  *capturefixture.CapturedSources
}

// NewCapturedSnapshots closes the capture store when the test ends.
func NewCapturedSnapshots(tb testing.TB, base string) (*parsing.SourceSnapshots, *capture.Store, *CapturedSnapshots) {
	tb.Helper()
	store, sources := capturefixture.NewCapturedSources(tb, base)
	registry, err := parsing.NewSourceSnapshots(store)
	if err != nil {
		tb.Fatal(err)
	}
	return registry, store, &CapturedSnapshots{registry, store, sources}
}

func (s *CapturedSnapshots) Path(path string) string { return s.sources.Path(path) }

// Capture captures content for path without admitting it into the registry.
func (s *CapturedSnapshots) Capture(path string, content []byte, coordinates sourcetext.Coordinates) (capture.CapturedDocument, error) {
	return s.sources.Capture(path, content, coordinates)
}

func (s *CapturedSnapshots) Document(path string, content []byte, document parsing.Document) (parsing.SourceSnapshot, error) {
	if path == "" {
		return parsing.SourceSnapshot{}, errors.New("fixture source identity is empty")
	}
	captured, err := s.sources.Capture(path, content, sourcetext.YAML)
	if err != nil {
		return parsing.SourceSnapshot{}, err
	}
	return s.registry.Admit(captured, document.Parsed)
}

func (s *CapturedSnapshots) Action(path string, metadata *actionmeta.Metadata) (parsing.SourceSnapshot, error) {
	if path == "" || metadata == nil {
		return parsing.SourceSnapshot{}, errors.New("fixture action source is unavailable")
	}
	text, parsed := metadata.Text()
	if !parsed {
		return parsing.SourceSnapshot{}, errors.New("fixture action was not parsed")
	}
	captured, err := s.sources.Capture(path, []byte(text), sourcetext.YAML)
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
