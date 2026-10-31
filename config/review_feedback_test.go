package config_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/config"
)

func TestPublicConfigurationConstructors(t *testing.T) {
	if _, err := config.ParseOverlay("tools", []byte("{shellcheck: false}")); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ParseOverlay("unknown-input", []byte("true")); err == nil {
		t.Fatal("unknown overlay input accepted")
	}
	_, err := config.Parse([]byte("lint: {rules: {policy: {disallow-suppressions: {level: on, options: {report: all, rules: [expression]}}}}}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Parse([]byte("lint: {rules: {policy: {disallow-suppressions: {level: on, options: {report: unknown}}}}}")); err == nil {
		t.Fatal("unknown suppression mode accepted")
	}
}

func TestExtendsErrorNamesPublicLoader(t *testing.T) {
	_, err := config.Parse([]byte("extends: [./base.yaml]\n"))
	if err == nil || !strings.Contains(err.Error(), "config.Load") {
		t.Fatalf("missing usable recovery instruction: %v", err)
	}
}
