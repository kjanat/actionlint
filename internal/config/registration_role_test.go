package config

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/rule"
)

func TestSelectedToolRegistrationReplacesBundledDescriptor(t *testing.T) {
	descriptor := rule.Descriptor{Name: "shellcheck", Category: "security", Provider: "shellcheck-rs", Maturity: "stable", Recommended: true}
	cfg, err := Parse([]byte("lint: {rules: {plugins: {shellcheck: on}}}"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveRegisteredRules(Presets{}, cfg, RuleRegistration{Descriptor: descriptor, ReplacesTool: "shellcheck"})
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
		if _, err := ResolveRegisteredRules(Presets{}, cfg, registration); err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Fatalf("registration %+v: expected collision, got %v", registration, err)
		}
	}
}
