package assembly

import (
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/execprotocol"
	"actionlint.kjanat.dev/v2/internal/input/references/github"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

// ResolverCommand names an executable that resolves remote dependencies over
// the resolver protocol. The zero value configures no resolver.
type ResolverCommand struct {
	Command   string
	Arguments []string
}

// GitHubAccess describes how a check reaches GitHub.
type GitHubAccess struct {
	// Getenv reads GITHUB_SERVER_URL, GH_TOKEN and GITHUB_TOKEN.
	Getenv func(string) string
	// Acquisition supplies the cache directory and the HTTP connections used
	// for action metadata and remote configuration.
	Acquisition dependencycache.Acquisition
	// Offline reads action metadata from the cache only and starts no resolver.
	Offline  bool
	Resolver ResolverCommand
}

// MetadataError reports that the action metadata provider could not be
// configured, for example because a token contains invalid characters.
type MetadataError struct{ Err error }

func (e *MetadataError) Error() string { return e.Err.Error() }

func (e *MetadataError) Unwrap() error { return e.Err }

// ConnectGitHub connects checking options to GitHub. The command line, the
// language server and the GitHub Action all call it, so the three entry points
// read the GitHub service, configure action metadata, start the remote
// dependency resolver and download configuration the same way.
//
// It returns options with the GitHub service, the action metadata provider,
// the resolver and the configuration connections that access selects. Every
// other field of options is returned unchanged. A failure to configure action
// metadata is a *MetadataError; an invalid GITHUB_SERVER_URL is returned as
// services reports it.
func ConnectGitHub(options checking.Options, access GitHubAccess) (checking.Options, error) {
	mode := github.Online
	if access.Offline {
		mode = github.Offline
	}
	// Without a cache directory, metadata lookups run uncached.
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
