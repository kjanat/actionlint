package assembly

import (
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/execprotocol"
	"actionlint.kjanat.dev/v2/internal/input/references/github"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

// ResolverCommand configures no resolver when it is the zero value.
type ResolverCommand struct {
	Command   string
	Arguments []string
}

type GitHubAccess struct {
	// Getenv reads GITHUB_SERVER_URL, GH_TOKEN and GITHUB_TOKEN.
	Getenv      func(string) string
	Acquisition dependencycache.Acquisition
	// Offline reads action metadata from the cache only and starts no resolver.
	Offline  bool
	Resolver ResolverCommand
}

type MetadataError struct{ Err error }

func (e *MetadataError) Error() string { return e.Err.Error() }

func (e *MetadataError) Unwrap() error { return e.Err }

// ConnectGitHub returns a *MetadataError when action metadata cannot be configured.
func ConnectGitHub(options checking.Options, access GitHubAccess) (checking.Options, error) {
	mode := github.Online
	if access.Offline {
		mode = github.Offline
	}
	cacheDirectory, _ := access.Acquisition.Directory()
	metadataOptions := github.EnvironmentOptions(mode, access.Getenv, cacheDirectory)
	metadataOptions.Transport = access.Acquisition.Connections.Transport
	if mode == github.Online && access.Resolver.Command != "" {
		resolver := access.Resolver
		options.Resolver = execprotocol.NewProcess(resolver.Command, resolver.Arguments, 0)
		options.SuggestCommitPins = true
		metadataOptions.Resolver = func() references.Resolver {
			return execprotocol.NewProcess(resolver.Command, resolver.Arguments, 0)
		}
	}
	metadata, err := github.New(metadataOptions)
	if err != nil {
		return options, &MetadataError{Err: err}
	}
	service, err := services.FromGitHubServerURL(access.Getenv("GITHUB_SERVER_URL"))
	if err != nil {
		return options, err
	}
	options.RemoteActions = metadata
	options.Service = service
	options.ConfigConnections = access.Acquisition.Connections
	return options, nil
}
