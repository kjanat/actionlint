package config_test

import (
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
)

func TestReusableIgnoreCannotSuppressPolicy(t *testing.T) {
	t.Parallel()
	_, err := checking.ConfigurationRules().Parse([]byte(`ignores: [{rule: disallow-suppressions, file: '*.yaml'}]`))
	if err == nil || !strings.Contains(err.Error(), "forbidden-suppression") {
		t.Fatalf("ineffective suppression was accepted: %v", err)
	}
}
