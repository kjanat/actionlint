package cli

import (
	"os"
	"path/filepath"

	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	resolverprocess "actionlint.kjanat.dev/v2/internal/input/references/process"
	"actionlint.kjanat.dev/v2/internal/integrations/github"
)

func (c checkInvocation) actionMetadata() (actions.Provider, error) {
	mode := github.Mode(c.ActionMetadata)
	if mode == "" {
		mode = github.Online
	}
	options := github.Options{Mode: mode}
	if mode != github.Off {
		if cache, err := os.UserCacheDir(); err == nil {
			options.CacheDir = filepath.Join(cache, "actionlint", "action-metadata", "v1")
		}
	}
	if mode == github.Online {
		options.Token = os.Getenv("GH_TOKEN")
		if options.Token == "" {
			options.Token = os.Getenv("GITHUB_TOKEN")
		}
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
