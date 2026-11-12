// Package shell prepares shell source with interpreter settings, declared
// environment names and source maps. Integrations supply their own settings
// conversions and preparation programs.
package shell

import (
	"actionlint.kjanat.dev/v2/internal/languages/bash"
	"actionlint.kjanat.dev/v2/internal/languages/interpreters"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

// ID names this profile in tool provider manifests.
const ID = "kjanat:actionlint/shell-preparation@0.3.0"

// Shell is the shell command a run block executes under. Implicit marks the
// runner's default shell, which omits pipefail.
type Shell struct {
	Name     string
	Implicit bool
	Registry *commands.Registry
	Platform commands.ArgumentPlatform
}

// Invocation parses and validates the command using the session's interpreter definitions.
func (s Shell) Invocation() commands.Invocation {
	registry := s.Registry
	if registry == nil {
		registry = interpreters.Builtin()
	}
	return registry.AnalyzeFor(s.Name, s.Implicit, s.Platform)
}

// Analysis supplies a dialect and startup settings only for a validated command
// that reads the run block as a script file.
func (s Shell) Analysis() (dialect, startup string) {
	invocation := s.Invocation()
	if invocation.Status != commands.Valid || invocation.Input != "file" {
		return "", ""
	}
	switch invocation.Language {
	case "bash", "sh", "dash", "ksh":
		return invocation.Language, bash.Setup(invocation.Settings)
	default:
		return "", ""
	}
}

// LanguageDialect is the dialect of an action input declared in language;
// empty when the language is not a supported shell.
func LanguageDialect(language string) string {
	switch language {
	case "bash", "sh", "dash", "ksh", "busybox":
		return language
	default:
		return ""
	}
}

// TargetArguments uses runner OS alternatives, independently of the host OS.
func TargetArguments(environments []scriptmodel.EnvironmentNames) commands.ArgumentPlatform {
	result := commands.UnknownArguments
	for _, environment := range environments {
		var current commands.ArgumentPlatform
		switch environment.OperatingSystem() {
		case runner.Linux, runner.MacOS:
			current = commands.UnixArguments
		case runner.Windows:
			current = commands.WindowsArguments
		case runner.UnknownOS:
			return commands.UnknownArguments
		default:
			return commands.UnknownArguments
		}
		if result != commands.UnknownArguments && result != current {
			return commands.UnknownArguments
		}
		result = current
	}
	return result
}
