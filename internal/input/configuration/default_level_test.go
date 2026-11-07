package config

import (
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestResolvedOnUsesDeclaredDefaultLevel(t *testing.T) {
	for _, declared := range []string{"info", "warn", "error", ""} {
		t.Run(declared, func(t *testing.T) {
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
