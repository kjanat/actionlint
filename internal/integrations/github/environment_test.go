package github

import "testing"

func TestEnvironmentOptionsModesAndCredentials(t *testing.T) {
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
		options := EnvironmentOptions(tc.mode, func(key string) string { return env[key] })
		mode := tc.mode
		if mode == "" {
			mode = Online
		}
		if options.Mode != mode || options.Token != tc.wantToken || mode == Off && options.CacheDir != "" {
			t.Fatalf("mode=%q: mode=%q cache=%q credential matched=%t", tc.mode, options.Mode, options.CacheDir, options.Token == tc.wantToken)
		}
	}
}
