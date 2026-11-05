package config

import (
	"strings"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func registrationRuleSet(t *testing.T) RuleSet {
	t.Helper()
	rules, err := NewRuleSet([]rule.Declaration{
		{Name: "shellcheck", Category: "correctness", Provider: "bundled", Maturity: "stable", Recommended: true},
		{Name: "ruff", Category: "correctness", Provider: "bundled", Maturity: "stable", Recommended: true},
		{Name: "expression", Category: "correctness", Provider: "actionlint", Maturity: "stable", Recommended: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestSelectedToolRegistrationReplacesBundledDescriptor(t *testing.T) {
	rules := registrationRuleSet(t)
	descriptor := rule.Descriptor{Name: "shellcheck", Category: "security", Provider: "shellcheck-rs", Maturity: "stable", Recommended: true}
	cfg, err := rules.Parse([]byte("lint: {rules: {plugins: {shellcheck: on}}}"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := rules.ResolveRegisteredRules(Presets{}, cfg, RuleRegistration{Descriptor: descriptor, ReplacesTool: "shellcheck"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Lint.Rules.boundCategories["shellcheck"]; got != "security" {
		t.Fatalf("selected replacement category = %q", got)
	}
	if level, ok := resolved.Lint.Rules.ResolvedLevel("shellcheck"); !ok || level != "on" {
		t.Fatalf("selected replacement level = %q, found %t", level, ok)
	}
	for _, registration := range []RuleRegistration{
		{Descriptor: descriptor},
		{Descriptor: descriptor, ReplacesTool: "ruff"},
		{Descriptor: rule.Descriptor{Name: "expression", Category: "correctness", Provider: "community"}, ReplacesTool: "expression"},
	} {
		if _, err := rules.ResolveRegisteredRules(Presets{}, cfg, registration); err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Fatalf("registration %+v: expected collision, got %v", registration, err)
		}
	}
}

func TestExperimentalToolRegistrationUsesExperimentalSelection(t *testing.T) {
	rules := registrationRuleSet(t)
	descriptor := rule.Descriptor{Name: "shellcheck", Category: "correctness", Provider: "shellcheck-rs", Maturity: rule.NurseryMaturity, Recommended: true}
	for _, tc := range []struct {
		name    string
		presets Presets
		want    Level
	}{
		{"default", Presets{}, "off"},
		{"strict", Presets{Strict: true}, "off"},
		{"experimental", Presets{Experimental: new(true)}, "on"},
		{"disabled experimental", Presets{Experimental: new(false)}, "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := rules.ResolveRegisteredRules(tc.presets, nil, RuleRegistration{Descriptor: descriptor, ReplacesTool: "shellcheck"})
			if err != nil {
				t.Fatal(err)
			}
			if level, ok := cfg.Lint.Rules.ResolvedLevel("shellcheck"); !ok || level != tc.want {
				t.Fatalf("ShellCheck maturity selection = %q, found %t; want %q", level, ok, tc.want)
			}
		})
	}
}
