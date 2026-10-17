package adapters

import (
	"errors"

	"actionlint.kjanat.dev/analyzer"
	toolconfig "actionlint.kjanat.dev/internal/config/tools"
)

func configuredScriptModule(name, requested string, explicit *ExternalCommandOptions, cfg *Config) *toolconfig.Module {
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
