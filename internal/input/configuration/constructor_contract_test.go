package config_test

import (
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
)

func TestConfigurationConstructors(t *testing.T) {
	t.Parallel()
	if _, err := checking.ConfigurationRules().ParseOverlay("tools", []byte("{shellcheck: false}")); err != nil {
		t.Fatal(err)
	}
	if _, err := checking.ConfigurationRules().ParseOverlay("unknown-input", []byte("true")); err == nil {
		t.Fatal("unknown overlay input accepted")
	}
	_, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {policy: {disallow-suppressions: {level: on, options: {report: all, rules: [expression]}}}}}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {policy: {disallow-suppressions: {level: on, options: {report: unknown}}}}}")); err == nil {
		t.Fatal("unknown suppression mode accepted")
	}
}

func TestExtendsErrorRequiresFilePath(t *testing.T) {
	t.Parallel()
	_, err := checking.ConfigurationRules().Parse([]byte("extends: [./base.yaml]\n"))
	if err == nil || !strings.Contains(err.Error(), "load the configuration file") {
		t.Fatalf("missing usable recovery instruction: %v", err)
	}
}
