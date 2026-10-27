// Package capture owns immutable source captures for one analysis result.
package capture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/source"
)

// Reader receives the Store lifetime context and canonical document address.
// Cancellation is cooperative. Close must join even a Reader ignoring ctx.
// The caller must not mutate returned bytes concurrently with their capture.
type Reader func(context.Context, string) ([]byte, error)

// Overlay supplies explicit bytes before any backing reads can begin.
type Overlay struct {
	Path        string
	Content     []byte
	Coordinates source.Coordinates
}

// Store canonicalizes each address once and retains successful and failed reads.
// Identity always uses host filesystem metadata via PhysicalPath, including when
// an injected Reader serves virtual content. Only Reader reads source bytes.
// Close must succeed before publication. On abort, cancel the lifetime context
// before Close; Close itself does not cancel tasks or interrupt filesystem calls.
type Store struct {
	ctx     context.Context
	reader  Reader
	resolve func(string) string
	mu      sync.Mutex
	aliases map[string]*alias
	docs    map[string]*entry
	active  int
	idle    chan struct{}
	closing bool
	sealed  bool
	sources []Source
}

type alias struct {
	done     chan struct{}
	document string
}

type entry struct {
	model    source.Coordinates
	aliases  map[string]struct{}
	done     chan struct{}
	value    *captured
	err      *CaptureError
	conflict *ConflictError
}

// New preflights all overlays before making the Store available. Conflicting
// documents are Store data, exposed by Read, Lookup and the sealed Sources table;
// they do not fail construction. Only malformed configuration returns an error.
// Overlay metadata resolution is synchronous and joined before New returns.
func New(ctx context.Context, overlays []Overlay, reader Reader) (*Store, error) {
	return newStoreWithResolver(ctx, overlays, reader, physicalDocument)
}

func physicalDocument(path string) string {
	if path == "<stdin>" {
		return path
	}
	return sourceid.Document(fsread.PhysicalPath(path))
}

func newStoreWithResolver(ctx context.Context, overlays []Overlay, reader Reader, resolve func(string) string) (*Store, error) {
	if ctx == nil || reader == nil {
		return nil, errors.New("source capture requires a lifetime context and reader")
	}
	for _, overlay := range overlays {
		if err := validatePath(overlay.Path); err != nil {
			return nil, err
		}
		if !overlay.Coordinates.Valid() {
			return nil, fmt.Errorf("source capture overlay %q requires coordinates", overlay.Path)
		}
	}
	idle := make(chan struct{})
	close(idle)
	s := &Store{ctx: ctx, reader: reader, resolve: resolve, aliases: make(map[string]*alias), docs: make(map[string]*entry), idle: idle}
	groups := make(map[string][]Overlay)
	for _, overlay := range overlays {
		// Store is not exposed yet; each synchronous resolution is fully joined.
		name := s.aliases[overlay.Path]
		if name == nil {
			s.startWork()
			name = &alias{done: make(chan struct{}), document: s.resolve(overlay.Path)}
			close(name.done)
			s.aliases[overlay.Path] = name
			s.finishWork()
		}
		document := name.document
		overlay.Content = bytes.Clone(overlay.Content)
		groups[document] = append(groups[document], overlay)
	}
	documents := make([]string, 0, len(groups))
	for document := range groups {
		documents = append(documents, document)
	}
	slices.Sort(documents)
	for _, document := range documents {
		group := groups[document]
		first := group[0]
		e := &entry{model: first.Coordinates, aliases: make(map[string]struct{}), done: make(chan struct{})}
		variants := make(map[string]struct{})
		for _, overlay := range group {
			e.aliases[overlay.Path] = struct{}{}
			variants[string(overlay.Coordinates)+":"+sourceid.Digest(overlay.Content)] = struct{}{}
		}
		if len(variants) != 1 {
			keys := make([]string, 0, len(variants))
			for variant := range variants {
				keys = append(keys, variant)
			}
			slices.Sort(keys)
			e.conflict = &ConflictError{document, "conflicting overlays: " + strings.Join(keys, ", ")}
		} else {
			value, err := capture(document, first.Coordinates, first.Content)
			e.value = value
			if err != nil {
				e.err = &CaptureError{document, err}
			}
		}
		close(e.done)
		s.docs[document] = e
	}
	return s, nil
}

func validatePath(path string) error {
	if path == "" || strings.ContainsRune(path, 0) {
		return errors.New("source capture requires a nonempty address without NUL")
	}
	if path != "<stdin>" && !filepath.IsAbs(path) {
		return errors.New("source capture requires an absolute address or <stdin>")
	}
	return nil
}

// startWork and finishWork require s.mu or exclusive constructor ownership.
func (s *Store) startWork() {
	if s.active == 0 {
		s.idle = make(chan struct{})
	}
	s.active++
}

func (s *Store) finishWork() {
	s.active--
	if s.active == 0 {
		close(s.idle)
	}
}

// Canonical resolves each alias once outside the global lock. Caller cancellation
// only cancels the wait; Close joins the metadata task. Discovered aliases,
// including aliases from cancelled or conflicting requests, enter Sources.
func (s *Store) Canonical(ctx context.Context, path string) (string, error) {
	if ctx == nil {
		return "", errors.New("source canonicalization requires a caller context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validatePath(path); err != nil {
		return "", err
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return "", err
	}
	name := s.aliases[path]
	if name == nil {
		if s.closing {
			s.mu.Unlock()
			return "", &ClosedError{}
		}
		if err := s.ctx.Err(); err != nil {
			s.mu.Unlock()
			return "", err
		}
		name = &alias{done: make(chan struct{})}
		s.aliases[path] = name
		s.startWork()
		go s.canonicalize(path, name)
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-s.ctx.Done():
		return "", s.ctx.Err()
	case <-name.done:
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return name.document, nil
}

func (s *Store) canonicalize(path string, name *alias) {
	document := s.resolve(path)
	s.mu.Lock()
	name.document = document
	e := s.docs[document]
	if e == nil {
		e = &entry{aliases: make(map[string]struct{})}
		s.docs[document] = e
	}
	e.aliases[path] = struct{}{}
	close(name.done)
	s.finishWork()
	s.mu.Unlock()
}

// Read shares one capture per canonical document. Caller cancellation only
// cancels its wait; backing IO belongs to the Store lifetime context.
func (s *Store) Read(ctx context.Context, path string, model source.Coordinates) (Snapshot, error) {
	if ctx == nil || !model.Valid() {
		return Snapshot{}, errors.New("source capture requires a caller context and coordinates")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	document, err := s.Canonical(ctx, path)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return Snapshot{}, &ClosedError{}
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	e := s.docs[document]
	if e.done == nil {
		if err := s.ctx.Err(); err != nil {
			s.mu.Unlock()
			return Snapshot{}, err
		}
		e.model, e.done = model, make(chan struct{})
		if document == "<stdin>" {
			e.err = &CaptureError{document, errors.New("stdin requires an overlay")}
			close(e.done)
		} else {
			s.startWork()
			go s.read(document, e)
		}
	}
	if e.model != model && e.conflict == nil {
		e.conflict = &ConflictError{document, "conflicting coordinates: raw-lf, yaml"}
	}
	if e.conflict != nil {
		conflict := e.conflict
		s.mu.Unlock()
		return Snapshot{}, conflict
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-s.ctx.Done():
		return Snapshot{}, s.ctx.Err()
	case <-e.done:
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.conflict != nil {
		return Snapshot{}, e.conflict
	}
	if e.err != nil {
		return Snapshot{}, e.err
	}
	return s.snapshot(e), nil
}

func (s *Store) read(document string, e *entry) {
	var content []byte
	err := s.ctx.Err()
	if err == nil {
		content, err = s.reader(s.ctx, document)
	}
	if lifetimeErr := s.ctx.Err(); lifetimeErr != nil {
		err = lifetimeErr
	}
	var value *captured
	if err == nil {
		value, err = capture(document, e.model, content)
	}
	s.mu.Lock()
	e.value = value
	if err != nil {
		e.err = &CaptureError{document, err}
	}
	close(e.done)
	s.finishWork()
	s.mu.Unlock()
}

func (s *Store) snapshot(e *entry) Snapshot {
	aliases := make([]string, 0, len(e.aliases))
	for alias := range e.aliases {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)
	return Snapshot{store: s, value: e.value, aliases: aliases}
}

// Verify rejects foreign, zero or stale handles and disagreeing supplied bytes.
// Verification never reads content, changes a capture or promotes other evidence.
func (s *Store) Verify(snapshot Snapshot, supplied []byte) error {
	if snapshot.store != s || snapshot.value == nil {
		return ErrInvalidSnapshot
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document := snapshot.value.revision.Document()
	e := s.docs[document]
	if e != nil && e.conflict != nil {
		return e.conflict
	}
	if e == nil || e.err != nil || e.value != snapshot.value || e.model != snapshot.value.model {
		return &ConflictError{document, "stale snapshot"}
	}
	if !bytes.Equal(snapshot.value.content, supplied) {
		return &ConflictError{document, "supplied bytes differ from captured revision"}
	}
	return nil
}

// Close stops new Read calls and metadata tasks, then joins every started task.
// It does not cancel the Store lifetime context. A failed join leaves Sources
// inaccessible; retry Close after the outstanding tasks finish.
func (s *Store) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("source capture close requires a caller context")
	}
	s.mu.Lock()
	s.closing = true
	idle := s.idle
	s.mu.Unlock()
	select {
	case <-idle:
	default:
		select {
		case <-idle:
		case <-ctx.Done():
			return &JoinError{ctx.Err()}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sealed {
		s.sources = s.sourceTable()
		s.sealed = true
	}
	return nil
}
