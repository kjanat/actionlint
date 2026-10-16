package engine

import (
	"context"
	"errors"

	"actionlint.kjanat.dev/internal/analyzerhost"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/toolconfig"
)

type ScriptEngines = analyzerhost.ScriptEngines

func NewScriptEngines(load func(context.Context, string) (analyzer.ScriptBackend, error)) *ScriptEngines {
	return analyzerhost.NewScriptEngines(load)
}

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
