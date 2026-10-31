package config

import (
	"strings"
	"testing"

	"github.com/kjanat/go-yaml/v4"
)

func TestDisallowSuppressionsConfig(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  SuppressionReport
	}{
		{"null", SuppressionsAllowed},
		{"false", SuppressionsAllowed},
		{"true", ReportAll},
		{"{}", ReportAll},
		{"{report: suppression}", ReportSuppression},
		{"{report: violation}", ReportViolation},
		{"{report: all}", ReportAll},
	} {
		t.Run(tc.value, func(t *testing.T) {
			cfg, err := Parse([]byte("policy: {disallow-suppressions: " + tc.value + "}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Policy.DisallowSuppressions.ReportFor("cache-operation"); got != tc.want {
				t.Fatalf("report = %v, want %v", got, tc.want)
			}
		})
	}
	var policy SuppressionsPolicy
	if err := yaml.Unmarshal([]byte("rules: [cache-operation, cache-operation]\nreport: violation"), &policy); err != nil {
		t.Fatal(err)
	}
	if policy.ReportFor("cache-operation") != ReportViolation || policy.ReportFor("cache-call-unrestricted") != SuppressionsAllowed || len(policy.rules) != 1 {
		t.Fatalf("rule selection: %+v", policy)
	}
	for _, value := range []string{"false", "true", "{}"} {
		if err := yaml.Unmarshal([]byte(value), &policy); err != nil {
			t.Fatal(err)
		}
		if len(policy.rules) != 0 {
			t.Fatalf("stale rules after %s: %+v", value, policy)
		}
	}
}

func TestDisallowSuppressionsInvalidConfig(t *testing.T) {
	for _, value := range []string{
		"'true'", "1", "[]", "{report: null}", "{report: true}", "{report: ignore}",
		"{rules: null}", "{rules: []}", "{rules: cache-operation}", "{rules: [1]}",
		"{rules: ['']}", "{rules: ['*']}", "{rules: [unknown-rule]}",
		"{rules: [cache-operation, typo]}",
		"{typo: true}", "{report: all, report: violation}",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := Parse([]byte("policy:\n  disallow-suppressions: " + value))
			if err == nil || !strings.Contains(err.Error(), "line") {
				t.Fatalf("expected positioned config error, got %v", err)
			}
		})
	}
}
