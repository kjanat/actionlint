package github

import (
	"os"
	"path/filepath"
)

// EnvironmentOptions uses the user's cache and GitHub credentials for metadata lookups.
func EnvironmentOptions(mode Mode, env func(string) string) Options {
	if mode == "" {
		mode = Online
	}
	options := Options{Mode: mode}
	if mode != Off {
		if cache, err := os.UserCacheDir(); err == nil {
			options.CacheDir = filepath.Join(cache, "actionlint", "action-metadata", "v2")
		}
	}
	if mode == Online {
		options.Token = env("GH_TOKEN")
		if options.Token == "" {
			options.Token = env("GITHUB_TOKEN")
		}
	}
	return options
}
