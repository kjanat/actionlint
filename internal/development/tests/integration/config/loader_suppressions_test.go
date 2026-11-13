package config_test

import (
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestDisallowSuppressionsConfig(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		want  repoconfig.SuppressionReport
	}{
		{"null", repoconfig.SuppressionsAllowed},
		{"false", repoconfig.SuppressionsAllowed},
		{"true", repoconfig.ReportAll},
		{"{}", repoconfig.ReportAll},
		{"{report: suppression}", repoconfig.ReportSuppression},
		{"{report: violation}", repoconfig.ReportViolation},
		{"{report: all}", repoconfig.ReportAll},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().Parse([]byte("policy: {disallow-suppressions: " + tc.value + "}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Policy.DisallowSuppressions.ReportFor("cache-operation"); got != tc.want {
				t.Fatalf("report = %v, want %v", got, tc.want)
			}
		})
	}
	var policy repoconfig.SuppressionsPolicy
	if err := decodeConfigYAML([]byte("rules: [cache-operation, cache-operation]\nreport: violation"), &policy); err != nil {
		t.Fatal(err)
	}
	if policy.ReportFor("cache-operation") != repoconfig.ReportViolation || policy.ReportFor("cache-call-unrestricted") != repoconfig.SuppressionsAllowed || len(policy.Rules()) != 1 {
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
			_, err := checking.ConfigurationRules().Parse([]byte("policy:\n  disallow-suppressions: " + value))
			if err == nil || !strings.Contains(err.Error(), "line") {
				t.Fatalf("expected positioned config error, got %v", err)
			}
		})
	}
}
