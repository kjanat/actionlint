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
// A nil error promises complete content; partial reads must return an error.
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
	done             chan struct{}
	document         string
	documentRequired bool
}

type entry struct {
	model       source.Coordinates
	aliases     map[string]struct{}
	readAliases map[string]struct{}
	done        chan struct{}
	value       *captured
	err         *CaptureError
	conflict    *ConflictError
}

// New preflights all overlays before making the Store available. Conflicting
// documents are Store data, exposed by Read, Lookup and the sealed Sources table;
// they do not fail construction. Failures return no Store and an InitError.
// A successful Store has no pending work. Cancellation stops the metadata wait,
// not the OS call; InitError owns any remaining work until Join succeeds.
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
		return nil, &InitError{cause: errors.New("source capture requires a lifetime context and reader")}
	}
	for _, overlay := range overlays {
		if err := validatePath(overlay.Path); err != nil {
			return nil, &InitError{cause: err}
		}
		if !overlay.Coordinates.Valid() {
			return nil, &InitError{cause: fmt.Errorf("source capture overlay %q requires coordinates", overlay.Path)}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, &InitError{cause: err}
	}
	idle := make(chan struct{})
	close(idle)
	s := &Store{ctx: ctx, reader: reader, resolve: resolve, aliases: make(map[string]*alias), docs: make(map[string]*entry), idle: idle}
	destinations := make([]string, len(overlays))
	for index, overlay := range overlays {
		document, err := s.Canonical(ctx, overlay.Path)
		if err != nil {
			return nil, s.initFailure(err)
		}
		destinations[index] = document
	}
	// Canonical closes its alias notification and finishes counted work under
	// one lock. Acquiring it here observes the final flight idle before mutation.
	s.mu.Lock()
	idle = s.idle
	s.mu.Unlock()
	select {
	case <-idle:
	case <-ctx.Done():
		return nil, s.initFailure(ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return nil, s.initFailure(err)
	}
	groups := make(map[string][]Overlay)
	for index, overlay := range overlays {
		overlay.Content = bytes.Clone(overlay.Content)
		document := destinations[index]
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
		e := s.docs[document]
		e.model, e.done = first.Coordinates, make(chan struct{})
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
	}
	if err := ctx.Err(); err != nil {
		return nil, s.initFailure(err)
	}
	return s, nil
}

func (s *Store) initFailure(cause error) *InitError {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
	failure := &InitError{cause: cause}
	if s.active != 0 {
		failure.pending = s.idle
	}
	return failure
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
	return s.canonical(ctx, path, true)
}

// CanonicalIdentity shares the alias memo and lifecycle without declaring a
// source document. Repository roots and other identity-only metadata never
// enter Sources. A later Canonical/Read can promote the alias before Close.
func (s *Store) CanonicalIdentity(ctx context.Context, path string) (string, error) {
	return s.canonical(ctx, path, false)
}

func (s *Store) canonical(ctx context.Context, path string, documentRequired bool) (string, error) {
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
		name = &alias{done: make(chan struct{}), documentRequired: documentRequired}
		s.aliases[path] = name
		s.startWork()
		go s.canonicalize(path, name)
	} else if documentRequired && !name.documentRequired {
		if s.closing {
			s.mu.Unlock()
			return "", &ClosedError{}
		}
		if err := s.ctx.Err(); err != nil {
			s.mu.Unlock()
			return "", err
		}
		name.documentRequired = true
		select {
		case <-name.done:
			s.registerDocument(path, name.document)
		default:
		}
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
	if name.documentRequired {
		s.registerDocument(path, document)
	}
	close(name.done)
	s.finishWork()
	s.mu.Unlock()
}

// registerDocument requires s.mu. Identity resolution and document promotion
// use the same alias flight, including when promotion arrives mid-resolution.
func (s *Store) registerDocument(path, document string) {
	e := s.docs[document]
	if e == nil {
		e = &entry{aliases: make(map[string]struct{})}
		s.docs[document] = e
	}
	e.aliases[path] = struct{}{}
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
	if e.readAliases == nil {
		e.readAliases = make(map[string]struct{})
	}
	e.readAliases[path] = struct{}{}
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
	if err := s.ctx.Err(); err != nil {
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

// VerifyHandle rejects foreign, zero, stale or quarantined capture handles.
// It checks owner identity without copying or comparing source bytes.
func (s *Store) VerifyHandle(snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifyHandle(snapshot)
}

// verifyHandle requires s.mu.
func (s *Store) verifyHandle(snapshot Snapshot) error {
	if snapshot.store != s || snapshot.value == nil {
		return ErrInvalidSnapshot
	}
	document := snapshot.value.revision.Document()
	e := s.docs[document]
	if e != nil && e.conflict != nil {
		return e.conflict
	}
	if e == nil || e.err != nil || e.value != snapshot.value || e.model != snapshot.value.model {
		return &ConflictError{document, "stale snapshot"}
	}
	return nil
}

// Verify rejects invalid handles and disagreeing externally supplied bytes.
// Verification never reads content, changes a capture or promotes other evidence.
func (s *Store) Verify(snapshot Snapshot, supplied []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyHandle(snapshot); err != nil {
		return err
	}
	if !bytes.Equal(snapshot.value.content, supplied) {
		return &ConflictError{snapshot.value.revision.Document(), "supplied bytes differ from captured revision"}
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
