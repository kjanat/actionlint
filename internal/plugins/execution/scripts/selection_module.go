package scripts

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	toolconfig "actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
)

// AllowsModule reports whether invocation settings permit configured Wasm selection.
func AllowsModule(name, requested string, explicit *process.CommandOptions) bool {
	return requested == name && (explicit == nil || !explicit.IgnoreConfig && (explicit.Optional || explicit.AllowWasm))
}

// Module returns the configured module unless an explicit process choice overrides it.
func Module(name, requested string, explicit *process.CommandOptions, cfg *configuration.Config) *toolconfig.Module {
	if cfg == nil || !AllowsModule(name, requested, explicit) {
		return nil
	}
	module, ok := cfg.Tools[name].Backend.Wasm()
	if !ok {
		return nil
	}
	return &module
}

// LoadConfiguredModule resolves a configured WebAssembly module through the invocation loader.
func LoadConfiguredModule(name, requested string, explicit *process.CommandOptions, cfg *configuration.Config, load func(string) (checkinputs.ScriptBackend, error)) (checkinputs.ScriptBackend, error) {
	module := Module(name, requested, explicit, cfg)
	if module == nil {
		return nil, nil
	}
	if load == nil {
		return nil, errors.New("tools." + name + ".backend.wasm selects " + module.Path() + ", and this host has no WebAssembly engine loader")
	}
	return load(module.Path())
}
