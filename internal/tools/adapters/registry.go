// Package adapters bundles the optional tools for application entrypoints.
// Each tool owns its registration, execution and source mapping in its own package.
package adapters

import (
	"actionlint.kjanat.dev/v2/analyzer"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/ruff"
	"actionlint.kjanat.dev/v2/internal/tools/shellcheck"
)

type Options struct {
	ShellCheck, Ruff               string
	ShellcheckOptions, RuffOptions *processconfig.Options
	ShellcheckBackend, RuffBackend analyzer.ScriptBackend
	ShellcheckRunner               func([]string, string) ([]byte, error)
	ShellcheckSettings             *toolconfig.ShellcheckSettings
}

func (o Options) Registrations() []tools.Registration {
	return []tools.Registration{
		(ruff.Options{Command: o.Ruff, Process: o.RuffOptions, Backend: o.RuffBackend}).Registration(),
		(shellcheck.Options{Command: o.ShellCheck, Process: o.ShellcheckOptions, Backend: o.ShellcheckBackend, Runner: o.ShellcheckRunner, Settings: o.ShellcheckSettings}).Registration(),
	}
}
