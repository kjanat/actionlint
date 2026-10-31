package cli

import (
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/actions"
	"actionlint.kjanat.dev/v2/internal/actionhost"
	"actionlint.kjanat.dev/v2/internal/resolution"
	"actionlint.kjanat.dev/v2/resolver"
)

func (c checkInvocation) actionMetadata() (actions.Provider, error) {
	mode := actionhost.Mode(c.ActionMetadata)
	if mode == "" {
		mode = actionhost.Off
	}
	options := actionhost.Options{Mode: mode}
	if mode != actionhost.Off {
		if cache, err := os.UserCacheDir(); err == nil {
			options.CacheDir = filepath.Join(cache, "actionlint", "action-metadata", "v1")
		}
	}
	if mode == actionhost.Online {
		options.Token = os.Getenv("GH_TOKEN")
		if options.Token == "" {
			options.Token = os.Getenv("GITHUB_TOKEN")
		}
		if c.ResolverCommand != "" {
			options.Resolver = func() resolver.Resolver { return resolution.NewProcess(c.ResolverCommand, c.ResolverArguments, 0) }
		}
	}
	provider, err := actionhost.New(options)
	if err != nil {
		return nil, commandUsageError{err}
	}
	return provider, nil
}
