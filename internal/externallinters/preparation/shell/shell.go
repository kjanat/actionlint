// Package shell prepares shell source with interpreter settings, declared
// environment names and source maps.
package shell

import (
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

const ID = "kjanat:actionlint/shell-preparation@0.3.0"

type Shell struct {
	scripts.Shell
	Registry *commands.Registry
	Platform commands.ArgumentPlatform
}

func (s Shell) Invocation() commands.Invocation {
	registry := s.Registry
	if registry == nil {
		registry = commands.BuiltinShells()
	}
	return registry.AnalyzeFor(s.Name, s.Implicit, s.Platform)
}

// Analysis returns empty settings unless the validated command reads the run block as a script file.
func (s Shell) Analysis() (dialect, startup string) {
	invocation := s.Invocation()
	if invocation.Status != commands.Valid || invocation.Input != "file" {
		return "", ""
	}
	switch invocation.Language {
	case "bash", "sh", "dash", "ksh":
		return invocation.Language, commands.BashSetup(invocation.Settings)
	default:
		return "", ""
	}
}

func LanguageDialect(language string) string {
	switch language {
	case "bash", "sh", "dash", "ksh", "busybox":
		return language
	default:
		return ""
	}
}

// TargetArguments uses runner OS alternatives, independently of the host OS.
func TargetArguments(environments []scripts.EnvironmentNames) commands.ArgumentPlatform {
	result := commands.UnknownArguments
	for _, environment := range environments {
		var current commands.ArgumentPlatform
		switch environment.OperatingSystem() {
		case runnerapi.Linux, runnerapi.MacOS:
			current = commands.UnixArguments
		case runnerapi.Windows:
			current = commands.WindowsArguments
		case runnerapi.UnknownOS:
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
