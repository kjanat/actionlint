package cli

import (
	"os"

	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/execprotocol"
	"actionlint.kjanat.dev/v2/internal/integrations/github"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

func (c checkInvocation) actionMetadata(acquisition pluginacquisition.Settings) (actions.Provider, error) {
	mode := github.Online
	if c.Offline {
		mode = github.Offline
	}
	options := github.EnvironmentOptions(mode, os.Getenv, acquisition.UserCacheDir)
	options.Transport = acquisition.Connections.Transport
	if options.Mode == github.Online {
		if c.ResolverCommand != "" {
			options.Resolver = func() references.Resolver {
				return execprotocol.NewProcess(c.ResolverCommand, c.ResolverArguments, 0)
			}
		}
	}
	provider, err := github.New(options)
	if err != nil {
		return nil, commandargs.UsageError{Cause: err}
	}
	return provider, nil
}
