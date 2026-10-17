package adapters

import (
	"maps"
	"slices"

	toolconfig "actionlint.kjanat.dev/internal/config/tools"
)

func projectToolOptions(name, requested string, explicit *ExternalCommandOptions, cfg *Config, cwd string) *ExternalCommandOptions {
	// An empty command is an explicit disable. Application/CLI/env choices win
	// over repository invocation settings, including an explicit default name.
	if requested != name || cfg == nil || explicit != nil && (explicit.IgnoreConfig || !explicit.Optional) {
		return explicit
	}
	var command *toolconfig.Command
	if name == "ruff" {
		if cfg.Tools.Ruff.Backend != nil {
			command = cfg.Tools.Ruff.Backend.Process
		}
	} else {
		if cfg.Tools.Shellcheck.Backend != nil {
			command = cfg.Tools.Shellcheck.Backend.Process
		}
	}
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
