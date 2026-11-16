package github

import (
	"path/filepath"
)

// EnvironmentOptions uses actionlint's cache directory and the user's GitHub
// credentials for metadata lookups. An empty cacheDirectory disables the
// metadata cache.
func EnvironmentOptions(mode Mode, env func(string) string, cacheDirectory string) Options {
	if mode == "" {
		mode = Online
	}
	options := Options{Mode: mode}
	if mode != Off && cacheDirectory != "" {
		options.CacheDir = filepath.Join(cacheDirectory, "action-metadata", "v2")
	}
	if mode == Online {
		options.Token = env("GH_TOKEN")
		if options.Token == "" {
			options.Token = env("GITHUB_TOKEN")
		}
	}
	return options
}
