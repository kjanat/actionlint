package actionlint

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestDisallowSuppressionsConfig(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  suppressionReport
	}{
		{"null", suppressionsAllowed}, {"false", suppressionsAllowed},
		{"true", reportBoth}, {"{}", reportBoth},
		{"{report: suppression}", reportSuppression},
		{"{report: violation}", reportViolation}, {"{report: both}", reportBoth},
	} {
		t.Run(tc.value, func(t *testing.T) {
			cfg, err := ParseConfig([]byte("policy: {disallow-suppressions: " + tc.value + "}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Policy.DisallowSuppressions.reportFor("cache-operation"); got != tc.want {
				t.Fatalf("report = %v, want %v", got, tc.want)
			}
		})
	}
	var policy SuppressionsPolicy
	if err := yaml.Unmarshal([]byte("rules: [cache-operation, cache-operation]\nreport: violation"), &policy); err != nil {
		t.Fatal(err)
	}
	if policy.reportFor("cache-operation") != reportViolation || policy.reportFor("cache-call-unrestricted") != suppressionsAllowed || len(policy.rules) != 1 {
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
		"{rules: ['']}", "{rules: ['*']}", "{rules: [expression]}",
		"{rules: [disallow-suppressions]}", "{rules: [cache-operation, typo]}",
		"{typo: true}", "{report: both, report: violation}",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseConfig([]byte("policy:\n  disallow-suppressions: " + value))
			if err == nil || !strings.Contains(err.Error(), "line") {
				t.Fatalf("expected positioned config error, got %v", err)
			}
		})
	}
}
