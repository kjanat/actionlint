package config_test

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

func TestConfigOverlayRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct{ input, value string }{
		{"config", ""},
		{"config", "[broken"},
		{"config", "{}\n---\n{}"},
		{"config", "unknown: true"},
		{"config", "self-hosted-runner: {lables: [typo]}"},
		{"policy", "required-action: [typo]"},
		{"paths", `"**": {ignores: [typo]}`},
		{"config-variables", "["},
		{"config-secrets", "{KEY: value}"},
		{"self-hosted-runner", "[wrong-shape]"},
		{"policy", "require-job-timeout: {min-minutes: 20, max-minutes: 10}"},
		{"assume-default-permissions", "unknown"},
		{"unknown", "null"},
	} {
		t.Run(tc.input+"/"+tc.value, func(t *testing.T) {
			_, err := repoconfig.ParseOverlay(tc.input, []byte(tc.value))
			if err == nil || !strings.Contains(err.Error(), tc.input) {
				t.Errorf("want error naming input %q, got %v", tc.input, err)
			}
		})
	}
}
