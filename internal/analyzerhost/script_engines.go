package analyzerhost

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"actionlint.kjanat.dev/v2/analyzer"
)

const maxScriptEngines = 4

var errScriptEnginesClosed = errors.New("WebAssembly engine session is closed")

type scriptEngineLoad struct {
	done    chan struct{}
	backend analyzer.ScriptBackend
	err     error
}

// ScriptEngines retains at most four distinct module paths, including failed loads.
type ScriptEngines struct {
	mu      sync.Mutex
	load    func(context.Context, string) (analyzer.ScriptBackend, error)
	loaded  map[string]analyzer.ScriptBackend
	failed  map[string]error
	loading map[string]*scriptEngineLoad
	done    chan struct{}
	closed  bool
}

// NewScriptEngines wraps a loader. A nil loader rejects every configured engine.
func NewScriptEngines(load func(context.Context, string) (analyzer.ScriptBackend, error)) *ScriptEngines {
	return &ScriptEngines{
		load: load, loaded: map[string]analyzer.ScriptBackend{}, failed: map[string]error{},
		loading: map[string]*scriptEngineLoad{}, done: make(chan struct{}),
	}
}

// Load returns the engine for a module path, loading it on first use.
// Loaders for distinct paths may run concurrently.
func (s *ScriptEngines) Load(ctx context.Context, path string) (analyzer.ScriptBackend, error) {
	if s == nil || s.load == nil {
		return nil, errors.New("this host has no WebAssembly engine loader")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errScriptEnginesClosed
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if backend, ok := s.loaded[path]; ok {
		s.mu.Unlock()
		return backend, nil
	}
	if err, ok := s.failed[path]; ok {
		s.mu.Unlock()
		return nil, err
	}
	if pending, ok := s.loading[path]; ok {
		s.mu.Unlock()
		return s.wait(ctx, pending)
	}
	if len(s.loaded)+len(s.failed)+len(s.loading) >= maxScriptEngines {
		s.mu.Unlock()
		return nil, fmt.Errorf("WebAssembly engine session limit of %d distinct module paths exceeded", maxScriptEngines)
	}
	pending := &scriptEngineLoad{done: make(chan struct{})}
	s.loading[path] = pending
	s.mu.Unlock()
	backend, err := s.load(ctx, path)
	if err == nil && backend == nil {
		err = fmt.Errorf("WebAssembly engine loader returned a nil backend for %q", path)
	}
	return s.finish(ctx, path, pending, backend, err)
}

func (s *ScriptEngines) wait(ctx context.Context, pending *scriptEngineLoad) (analyzer.ScriptBackend, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, errScriptEnginesClosed
	case <-pending.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return nil, errScriptEnginesClosed
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return pending.backend, pending.err
	}
}

func (s *ScriptEngines) finish(ctx context.Context, path string, pending *scriptEngineLoad, backend analyzer.ScriptBackend, err error) (analyzer.ScriptBackend, error) {
	s.mu.Lock()
	delete(s.loading, path)
	if s.closed {
		s.mu.Unlock()
		if closer, ok := backend.(interface{ Close(context.Context) error }); ok {
			err = errors.Join(err, closer.Close(context.WithoutCancel(ctx)))
		}
		err = errors.Join(errScriptEnginesClosed, err)
		s.mu.Lock()
		pending.err = err
		close(pending.done)
		s.mu.Unlock()
		return nil, err
	}
	if err != nil {
		s.failed[path] = err
	} else {
		s.loaded[path] = backend
		pending.backend = backend
	}
	pending.err = err
	close(pending.done)
	s.mu.Unlock()
	return pending.backend, err
}

// Close releases every loaded engine that can be closed and rejects further loads.
func (s *ScriptEngines) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.done != nil {
		close(s.done)
	}
	loaded := s.loaded
	s.loaded = nil
	clear(s.failed)
	clear(s.loading)
	s.mu.Unlock()
	var errs []error
	for _, backend := range loaded {
		if closer, ok := backend.(interface{ Close(context.Context) error }); ok {
			errs = append(errs, closer.Close(ctx))
		}
	}
	return errors.Join(errs...)
}
