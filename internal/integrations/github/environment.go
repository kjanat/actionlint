package github

import (
	"os"
	"path/filepath"
)

// EnvironmentOptions uses the user's cache and GitHub credentials for metadata lookups.
func EnvironmentOptions(mode Mode, env func(string) string, userCacheDir string) Options {
	if mode == "" {
		mode = Online
	}
	options := Options{Mode: mode}
	if mode != Off {
		if userCacheDir == "" {
			userCacheDir, _ = os.UserCacheDir()
		}
		if userCacheDir != "" {
			options.CacheDir = filepath.Join(userCacheDir, "actionlint", "action-metadata", "v2")
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
