package config

import (
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestLifecycleFixedDiagnosticLevel(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet([]rule.Declaration{{Name: "example", Category: "correctness", Maturity: "stable", Recommended: true, DefaultLevel: "warn"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, config string
		strict       bool
		level        Level
		fixed        bool
	}{
		{"default", "{}", false, "warn", false},
		{"on", "lint: {rules: {correctness: {example: on}}}", false, "warn", false},
		{"explicit default", "lint: {rules: {correctness: {example: default}}}", false, "warn", false},
		{"warning", "lint: {rules: {correctness: {example: warn}}}", false, "warn", true},
		{"error", "lint: {rules: {correctness: {example: error}}}", false, "error", true},
		{"off", "lint: {rules: {correctness: {example: off}}}", false, "off", false},
		{"strict", "{}", true, "error", true},
		{"on overrides strict", "lint: {rules: {correctness: {example: on}}}", true, "warn", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, e := rules.Parse([]byte(tc.config))
			if e != nil {
				t.Fatal(e)
			}
			cfg, e = rules.ResolveRules(Presets{Strict: tc.strict}, cfg)
			if e != nil {
				t.Fatal(e)
			}
			level, fixed := rules.FixedDiagnosticLevel(cfg, "example")
			if level != tc.level || fixed != tc.fixed {
				t.Fatalf("level=%q fixed=%t, want %q %t", level, fixed, tc.level, tc.fixed)
			}
		})
	}
}
