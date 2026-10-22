package engine

import (
	"fmt"
	"maps"
	"slices"

	"actionlint.kjanat.dev/internal/toolconfig"
)

func (c ruleContext) requireConfigProcessOptIn(name, requested string, explicit *ExternalCommandOptions, cfg *Config) error {
	if !c.allowConfigProcesses && projectToolCommand(name, requested, explicit, cfg) != nil {
		return fmt.Errorf("tools.%s.backend.process requires host opt-in; use --allow-config-processes for trusted repository configuration", name)
	}
	return nil
}

func projectToolCommand(name, requested string, explicit *ExternalCommandOptions, cfg *Config) *toolconfig.Command {
	// An empty command is an explicit disable. Application/CLI/env choices win
	// over repository invocation settings, including an explicit default name.
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
	return backend.Process
}

func projectToolOptions(name, requested string, explicit *ExternalCommandOptions, cfg *Config, cwd string) *ExternalCommandOptions {
	command := projectToolCommand(name, requested, explicit, cfg)
	if command == nil {
		return explicit
	}
	options := &ExternalCommandOptions{WorkingDir: cwd, LauncherArguments: slices.Clone(command.Args)}
	if command.Executable != "" {
		options.Executable = new(command.Executable)
	}
	for _, key := range slices.Sorted(maps.Keys(command.Env)) {
		options.Environment = append(options.Environment, key+"="+command.Env[key])
	}
	// Legacy --pyflakes selects F even when a repository supplies the executable.
	if explicit != nil {
		options.Arguments = append(options.Arguments, explicit.Arguments...)
		options.Environment = append(options.Environment, explicit.Environment...)
	}
	return options
}
