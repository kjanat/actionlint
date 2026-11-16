package github

import (
	"path/filepath"
	"testing"
)

func TestEnvironmentOptionsModesAndCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode                  Mode
		gh, github, wantToken string
	}{
		{"", "primary", "fallback", "primary"},
		{Online, "", "fallback", "fallback"},
		{Offline, "primary", "fallback", ""},
		{Off, "primary", "fallback", ""},
	} {
		env := map[string]string{"GH_TOKEN": tc.gh, "GITHUB_TOKEN": tc.github}
		options := EnvironmentOptions(tc.mode, func(key string) string { return env[key] }, "")
		mode := tc.mode
		if mode == "" {
			mode = Online
		}
		if options.Mode != mode || options.Token != tc.wantToken || mode == Off && options.CacheDir != "" {
			t.Fatalf("mode=%q: mode=%q cache=%q credential matched=%t", tc.mode, options.Mode, options.CacheDir, options.Token == tc.wantToken)
		}
	}
	if options := EnvironmentOptions(Online, func(string) string { return "" }, ""); options.CacheDir != "" {
		t.Fatalf("cache directory = %q without an actionlint cache directory", options.CacheDir)
	}
	selected := t.TempDir()
	if options := EnvironmentOptions(Online, func(string) string { return "" }, selected); options.CacheDir != filepath.Join(selected, "action-metadata", "v2") {
		t.Fatalf("selected cache directory = %q", options.CacheDir)
	}
}
