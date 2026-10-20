package selection

import (
	"fmt"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

// RequireProcessOptIn rejects repository-selected executables without host authorization.
func RequireProcessOptIn(name, requested string, explicit *processconfig.Options, cfg *config.Config, allowed bool) error {
	if !allowed && processCommand(name, requested, explicit, cfg) != nil {
		return fmt.Errorf("tools.%s.backend.process requires host opt-in; use --allow-config-processes for trusted repository configuration", name)
	}
	return nil
}

func processCommand(name, requested string, explicit *processconfig.Options, cfg *config.Config) *toolconfig.Command {
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

// ProcessOptions applies repository backend settings unless the caller made an explicit choice.
func ProcessOptions(name, requested string, explicit *processconfig.Options, cfg *config.Config, cwd string) *processconfig.Options {
	command := processCommand(name, requested, explicit, cfg)
	if command == nil {
		return explicit
	}
	options := &processconfig.Options{WorkingDir: cwd, LauncherArguments: slices.Clone(command.Args)}
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
