package cli

import (
	"errors"
	"io"
	"os"

	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/cli/checkoptions"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

// checkingOptions builds the checking options that `check` and `lsp` share
// from the flags and environment both commands read. legacy selects the
// legacy grammar, in which an explicit configuration file does not replace
// the project's configuration. Each command sets its own remaining fields.
func (c checkInvocation) checkingOptions(workingDir string, acquisition pluginacquisition.Settings, log io.Writer, legacy bool) (checking.Options, error) {
	options := checking.Options{
		WorkingDir:           workingDir,
		ConfigFile:           c.Config.Path,
		SkipProjectConfig:    c.Config.Disabled || (!legacy && c.Config.Path != ""),
		EnforcePolicies:      c.EnforcePolicies,
		AllowConfigProcesses: c.AllowConfigProcesses,
		LogWriter:            log,
		Tools:                registry.Registrations(c.Tools),
	}
	options, err := checkoptions.ConnectGitHub(options, checkoptions.GitHubAccess{
		Getenv:      os.Getenv,
		Acquisition: acquisition,
		Offline:     c.Offline,
		Resolver:    checkoptions.Resolver{Command: c.ResolverCommand, Arguments: c.ResolverArguments},
	})
	if err != nil {
		if _, ok := errors.AsType[*checkoptions.MetadataError](err); ok {
			return options, commandargs.UsageError{Cause: err}
		}
		return options, err
	}
	options.Runners, err = c.runners()
	return options, err
}
