package backend

import (
	"errors"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

// AllowsModule reports whether invocation settings permit configured Wasm selection.
func AllowsModule(name, requested string, explicit *processconfig.Options) bool {
	return requested == name && (explicit == nil || !explicit.IgnoreConfig && (explicit.Optional || explicit.AllowWasm))
}

// Module returns the configured module unless an explicit process choice overrides it.
func Module(name, requested string, explicit *processconfig.Options, cfg *config.Config) *toolconfig.Module {
	if cfg == nil || !AllowsModule(name, requested, explicit) {
		return nil
	}
	module, ok := cfg.Tools[name].Backend.Wasm()
	if !ok {
		return nil
	}
	return &module
}

// Load resolves a configured WebAssembly module through the invocation loader.
func Load(name, requested string, explicit *processconfig.Options, cfg *config.Config, load func(string) (analyzer.ScriptBackend, error)) (analyzer.ScriptBackend, error) {
	module := Module(name, requested, explicit, cfg)
	if module == nil {
		return nil, nil
	}
	if load == nil {
		return nil, errors.New("tools." + name + ".backend.wasm selects " + module.Path() + ", and this host has no WebAssembly engine loader")
	}
	return load(module.Path())
}
