package cli

import (
	"os"

	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	resolverprocess "actionlint.kjanat.dev/v2/internal/input/references/process"
	"actionlint.kjanat.dev/v2/internal/integrations/github"
)

func (c checkInvocation) actionMetadata() (actions.Provider, error) {
	options := github.EnvironmentOptions(github.Mode(c.ActionMetadata), os.Getenv)
	if options.Mode == github.Online {
		if c.ResolverCommand != "" {
			options.Resolver = func() references.Resolver {
				return resolverprocess.NewProcess(c.ResolverCommand, c.ResolverArguments, 0)
			}
		}
	}
	provider, err := github.New(options)
	if err != nil {
		return nil, commandargs.UsageError{Cause: err}
	}
	return provider, nil
}
