package config_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestDisallowSuppressionsConfig(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		want  configuration.SuppressionReport
	}{
		{"null", configuration.SuppressionsAllowed},
		{"false", configuration.SuppressionsAllowed},
		{"true", configuration.ReportAll},
		{"{}", configuration.ReportAll},
		{"{report: suppression}", configuration.ReportSuppression},
		{"{report: violation}", configuration.ReportViolation},
		{"{report: all}", configuration.ReportAll},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().ParseConfig([]byte("policy: {disallow-suppressions: " + tc.value + "}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Policy.DisallowSuppressions.ReportFor("cache-operation"); got != tc.want {
				t.Fatalf("report = %v, want %v", got, tc.want)
			}
		})
	}
	var policy configuration.SuppressionsPolicy
	if err := decodeConfigYAML([]byte("rules: [cache-operation, cache-operation]\nreport: violation"), &policy); err != nil {
		t.Fatal(err)
	}
	if policy.ReportFor("cache-operation") != configuration.ReportViolation || policy.ReportFor("cache-call-unrestricted") != configuration.SuppressionsAllowed || len(policy.Rules()) != 1 {
		t.Fatalf("rule selection: %+v", policy)
	}
	for _, value := range []string{"false", "true", "{}"} {
		if err := decodeConfigYAML([]byte(value), &policy); err != nil {
			t.Fatal(err)
		}
		if len(policy.Rules()) != 0 {
			t.Fatalf("stale rules after %s: %+v", value, policy)
		}
	}
}

func TestDisallowSuppressionsInvalidConfig(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"'true'", "1", "[]", "{report: null}", "{report: true}", "{report: ignore}",
		"{rules: null}", "{rules: []}", "{rules: cache-operation}", "{rules: [1]}",
		"{rules: ['']}", "{rules: ['*']}", "{rules: [unknown-rule]}",
		"{rules: [cache-operation, typo]}",
		"{typo: true}", "{report: all, report: violation}",
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			_, err := checking.ConfigurationRules().ParseConfig([]byte("policy:\n  disallow-suppressions: " + value))
			if err == nil || !strings.Contains(err.Error(), "line") {
				t.Fatalf("expected positioned config error, got %v", err)
			}
		})
	}
}
