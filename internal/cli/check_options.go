package cli

import (
	"errors"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"

	"actionlint.kjanat.dev/v2/internal/assembly"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
)

// checkingOptions with legacy set keeps the project's configuration beside an
// explicit configuration file.
func (c checkInvocation) checkingOptions(workingDir string, acquisition dependencycache.Acquisition, log io.Writer, legacy bool) (checking.Options, error) {
	options := checking.Options{
		WorkingDir:           workingDir,
		ConfigFile:           c.Config.Path,
		SkipProjectConfig:    c.Config.Disabled || (!legacy && c.Config.Path != ""),
		EnforcePolicies:      c.EnforcePolicies,
		AllowConfigProcesses: c.AllowConfigProcesses,
		ConfigSources:        c.ConfigSources,
		LogWriter:            log,
		Tools:                registry.Registrations(c.Tools),
	}
	options, err := assembly.ConnectGitHub(options, assembly.GitHubAccess{
		Getenv:      os.Getenv,
		Acquisition: acquisition,
		Offline:     c.Offline,
		Resolver:    assembly.ResolverCommand{Command: c.ResolverCommand, Arguments: c.ResolverArguments},
	})
	if err != nil {
		if _, ok := errors.AsType[*assembly.MetadataError](err); ok {
			return options, exitcode.UsageError{Cause: err}
		}
		return options, err
	}
	options.Runners, err = c.runners()
	return options, err
}
