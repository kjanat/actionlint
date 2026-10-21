package engine

import (
	"context"
	"errors"
	"sync"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/toolconfig"
)

// ScriptEngines loads each configured WebAssembly engine once and closes them together.
type ScriptEngines struct {
	mu     sync.Mutex
	load   func(context.Context, string) (analyzer.ScriptBackend, error)
	loaded map[string]analyzer.ScriptBackend
	failed map[string]error
}

// NewScriptEngines wraps a loader. A nil loader rejects every configured engine.
func NewScriptEngines(load func(context.Context, string) (analyzer.ScriptBackend, error)) *ScriptEngines {
	return &ScriptEngines{load: load, loaded: map[string]analyzer.ScriptBackend{}, failed: map[string]error{}}
}

// Load returns the engine for a module path, loading it on first use.
func (s *ScriptEngines) Load(ctx context.Context, path string) (analyzer.ScriptBackend, error) {
	if s == nil || s.load == nil {
		return nil, errors.New("this host has no WebAssembly engine loader")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if backend, ok := s.loaded[path]; ok {
		return backend, nil
	}
	if err, ok := s.failed[path]; ok {
		return nil, err
	}
	backend, err := s.load(ctx, path)
	if err != nil {
		s.failed[path] = err
		return nil, err
	}
	s.loaded[path] = backend
	return backend, nil
}

// Close releases every loaded engine that can be closed.
func (s *ScriptEngines) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for path, backend := range s.loaded {
		if closer, ok := backend.(interface{ Close(context.Context) error }); ok {
			errs = append(errs, closer.Close(ctx))
		}
		delete(s.loaded, path)
	}
	return errors.Join(errs...)
}

func configuredScriptSelectionEnabled(name, requested string, explicit *ExternalCommandOptions) bool {
	return requested == name && (explicit == nil || !explicit.IgnoreConfig && (explicit.Optional || explicit.AllowWasm))
}

func configuredScriptModule(name, requested string, explicit *ExternalCommandOptions, cfg *Config) *toolconfig.Module {
	if cfg == nil || !configuredScriptSelectionEnabled(name, requested, explicit) {
		return nil
	}
	backend := cfg.Tools.Shellcheck.Backend
	if name == "ruff" {
		backend = cfg.Tools.Ruff.Backend
	}
	if backend == nil {
		return nil
	}
	return backend.Wasm
}

func (c ruleContext) configuredScriptEngine(name, requested string, explicit *ExternalCommandOptions) (analyzer.ScriptBackend, error) {
	module := configuredScriptModule(name, requested, explicit, c.config)
	if module == nil {
		return nil, nil
	}
	if c.scriptEngine == nil {
		return nil, errors.New("tools." + name + ".backend.wasm selects " + module.Path + ", and this host has no WebAssembly engine loader")
	}
	return c.scriptEngine(module.Path)
}
