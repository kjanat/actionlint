package configuration_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
)

func TestReusableIgnoreCannotSuppressPolicy(t *testing.T) {
	t.Parallel()
	_, err := checking.ConfigurationRules().ParseConfig([]byte(`ignores: [{rule: disallow-suppressions, file: '*.yaml'}]`))
	if err == nil || !strings.Contains(err.Error(), "forbidden-suppression") {
		t.Fatalf("ineffective suppression was accepted: %v", err)
	}
}
