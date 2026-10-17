package selection

import (
	"errors"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/config"
	toolconfig "actionlint.kjanat.dev/internal/config/tools"
	"actionlint.kjanat.dev/internal/processconfig"
)

// Module returns the configured module unless an explicit process choice overrides it.
func Module(name, requested string, explicit *processconfig.Options, cfg *config.Config) *toolconfig.Module {
	if requested != name || cfg == nil || explicit != nil && (explicit.IgnoreConfig || !explicit.Optional) {
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

// Load resolves a configured WebAssembly module through the invocation loader.
func Load(name, requested string, explicit *processconfig.Options, cfg *config.Config, load func(string) (analyzer.ScriptBackend, error)) (analyzer.ScriptBackend, error) {
	module := Module(name, requested, explicit, cfg)
	if module == nil {
		return nil, nil
	}
	if load == nil {
		return nil, errors.New("tools." + name + ".backend.wasm selects " + module.Path + ", and this host has no WebAssembly engine loader")
	}
	return load(module.Path)
}
