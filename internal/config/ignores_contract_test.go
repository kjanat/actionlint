package config_test

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestReusableIgnoreCannotSuppressPolicy(t *testing.T) {
	_, err := repoconfig.Parse([]byte(`ignores: [{rule: disallow-suppressions, file: '*.yaml'}]`))
	if err == nil || !strings.Contains(err.Error(), "disallow-suppressions") {
		t.Fatalf("ineffective suppression was accepted: %v", err)
	}
}
