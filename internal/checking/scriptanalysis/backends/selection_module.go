package backends

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

func AllowsModule(name, requested string, explicit *hostprocess.CommandOptions) bool {
	return requested == name && (explicit == nil || !explicit.IgnoreConfig && (explicit.Optional || explicit.AllowWasm))
}

func Module(name, requested string, explicit *hostprocess.CommandOptions, cfg *configuration.Config) *tools.Module {
	if cfg == nil || !AllowsModule(name, requested, explicit) {
		return nil
	}
	module, ok := cfg.Tools[name].Backend.Wasm()
	if !ok {
		return nil
	}
	return &module
}

func LoadConfiguredModule(name, requested string, explicit *hostprocess.CommandOptions, cfg *configuration.Config, load func(string) (scriptengine.Backend, error)) (scriptengine.Backend, error) {
	module := Module(name, requested, explicit, cfg)
	if module == nil {
		return nil, nil
	}
	if load == nil {
		return nil, errors.New("tools." + name + ".backend.wasm selects " + module.Path() + ", and this host has no WebAssembly engine loader")
	}
	return load(module.Path())
}
