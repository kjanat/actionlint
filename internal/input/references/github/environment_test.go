package github

import (
	"os"
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
	want := ""
	if cache, err := os.UserCacheDir(); err == nil {
		want = filepath.Join(cache, "actionlint", "action-metadata", "v2")
	}
	if options := EnvironmentOptions(Online, func(string) string { return "" }, ""); options.CacheDir != want {
		t.Fatalf("cache directory = %q, want %q", options.CacheDir, want)
	}
	selected := t.TempDir()
	if options := EnvironmentOptions(Online, func(string) string { return "" }, selected); options.CacheDir != filepath.Join(selected, "actionlint", "action-metadata", "v2") {
		t.Fatalf("selected cache directory = %q", options.CacheDir)
	}
}
