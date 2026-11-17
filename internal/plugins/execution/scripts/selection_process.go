package scripts

import (
	"fmt"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	toolconfig "actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
)

// RequireProcessOptIn rejects repository-selected executables without host authorization.
func RequireProcessOptIn(name, requested string, explicit *process.CommandOptions, cfg *configuration.Config, allowed bool) error {
	if !allowed && processCommand(name, requested, explicit, cfg) != nil {
		return fmt.Errorf("tools.%s.backend.process requires host opt-in; use --allow-config-processes for trusted repository configuration", name)
	}
	return nil
}

func processCommand(name, requested string, explicit *process.CommandOptions, cfg *configuration.Config) *toolconfig.Command {
	// An empty command is an explicit disable. Application/CLI/env choices win
	// over repository invocation settings, including an explicit default name.
	if requested != name || cfg == nil || explicit != nil && (explicit.IgnoreConfig || !explicit.Optional) {
		return nil
	}
	command, ok := cfg.Tools[name].Backend.Process()
	if !ok {
		return nil
	}
	return &command
}

// ProcessOptions applies repository backend settings unless the caller made an explicit choice.
func ProcessOptions(name, requested string, explicit *process.CommandOptions, cfg *configuration.Config, cwd string) *process.CommandOptions {
	command := processCommand(name, requested, explicit, cfg)
	if command == nil {
		return explicit
	}
	options := &process.CommandOptions{WorkingDir: cwd, LauncherArguments: command.Args()}
	if executable := command.Executable(); executable != "" {
		options.Executable = new(executable)
	}
	env := command.Env()
	for _, key := range slices.Sorted(maps.Keys(env)) {
		options.Environment = append(options.Environment, key+"="+env[key])
	}
	// Arguments a retired command-line flag adds still apply when a repository
	// supplies the executable.
	if explicit != nil {
		options.Arguments = append(options.Arguments, explicit.Arguments...)
		options.Environment = append(options.Environment, explicit.Environment...)
	}
	return options
}
