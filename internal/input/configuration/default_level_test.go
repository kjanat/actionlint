package config

import (
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestResolvedOnUsesDeclaredDefaultLevel(t *testing.T) {
	t.Parallel()
	for _, declared := range []string{"info", "warn", "error", ""} {
		t.Run(declared, func(t *testing.T) {
			t.Parallel()
			rules, err := NewRuleSet([]rule.Declaration{{Name: "example", Category: "correctness", Maturity: "stable", Recommended: true, DefaultLevel: declared}})
			if err != nil {
				t.Fatal(err)
			}
			wantDefault := Level(declared)
			if declared == "" {
				wantDefault = "on"
			}
			for _, test := range []struct {
				name, yaml string
				presets    Presets
				want       Level
			}{
				{"default", "{}", Presets{}, wantDefault},
				{"rule on", "lint: {rules: {correctness: {example: on}}}", Presets{}, wantDefault},
				{"group on", "lint: {rules: {correctness: on}}", Presets{}, wantDefault},
				{"all preset", "lint: {rules: {preset: all}}", Presets{}, wantDefault},
				{"explicit warning", "lint: {rules: {correctness: {example: warn}}}", Presets{}, "warn"},
				{"explicit off", "lint: {rules: {correctness: {example: off}}}", Presets{Strict: true}, "off"},
				{"explicit on overrides strict", "lint: {rules: {correctness: {example: on}}}", Presets{Strict: true}, wantDefault},
			} {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()
					cfg, err := rules.Parse([]byte(test.yaml))
					if err != nil {
						t.Fatal(err)
					}
					resolved, err := rules.ResolveRules(test.presets, cfg)
					if err != nil {
						t.Fatal(err)
					}
					level, found := resolved.Lint.Rules.ResolvedLevel("example")
					if !found || level != test.want || rules.DiagnosticLevel(resolved, "example") != test.want {
						t.Fatalf("resolved=%q, found=%t, diagnostic=%q; want %q", level, found, rules.DiagnosticLevel(resolved, "example"), test.want)
					}
				})
			}
		})
	}
}

func TestRuleMetadataDefaultsIgnoreInvocationOverrides(t *testing.T) {
	t.Parallel()
	declarations := []rule.Declaration{
		{Name: "information", Category: "correctness", Maturity: "stable", Recommended: true, DefaultLevel: "info"},
		{Name: "optional", Category: "suspicious", Maturity: "stable", DefaultLevel: "warn"},
		{Name: "nursery", Category: "suspicious", Maturity: "nursery", Recommended: true, DefaultLevel: "warn"},
		{Name: "ordinary", Category: "correctness", Maturity: "stable", Recommended: true},
	}
	rules, err := NewRuleSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	base := rules
	plugin := rule.Descriptor{Name: "plugin", Category: "security", Maturity: "stable", DefaultLevel: "warn"}
	rules, err = rules.WithRegisteredRules(RuleRegistration{Descriptor: plugin})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := rules.Parse([]byte("lint: {rules: {correctness: error, suspicious: {optional: error, nursery: error}, security: {plugin: error}}}"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := base.ResolveRegisteredRules(Presets{}, cfg, RuleRegistration{Descriptor: plugin})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, level string
		enabled     bool
	}{
		{"information", "info", true}, {"optional", "warn", false}, {"nursery", "warn", false}, {"ordinary", "", true}, {"plugin", "warn", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry, ok := rules.FindRule(tc.name)
			if !ok {
				t.Fatal("rule missing")
			}
			got := rules.WithDefaults(entry.Descriptor)
			if got.DefaultLevel != tc.level || got.DefaultEnabled == nil || *got.DefaultEnabled != tc.enabled {
				t.Fatalf("default metadata = %q, %v; want %q, %t", got.DefaultLevel, got.DefaultEnabled, tc.level, tc.enabled)
			}
			if level := rules.DiagnosticLevel(resolved, tc.name); level != "error" {
				t.Fatalf("invocation level = %q, want error", level)
			}
		})
	}
}
