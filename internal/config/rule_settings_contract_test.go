package config_test

import (
	"fmt"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestCatalogRuleConfiguration(t *testing.T) {
	for _, d := range repoconfig.Rules() {
		t.Run(d.Name, func(t *testing.T) {
			cfg, err := repoconfig.Parse(fmt.Appendf(nil, "lint: {rules: {%s: {%s: off}}}", d.Group, d.Name))
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []repoconfig.Presets{{}, {Strict: true, Experimental: new(true)}} {
				if got := repoconfig.DiagnosticLevel(repoconfig.ResolveRules(p, cfg), d.Name); got != "off" {
					t.Fatalf("explicit off overridden: %s", got)
				}
			}
			if _, ok := cfg.Lint.Rules.ResolvedLevel(d.Name); ok {
				t.Fatal("shared config mutated")
			}
			if d.Maturity == "nursery" && d.Recommended {
				t.Fatal("nursery rule marked recommended")
			}
		})
	}
}

func TestRuleLevelAliasesAndDefaults(t *testing.T) {
	for _, tc := range []struct {
		rules string
		name  string
		want  repoconfig.Level
	}{
		{"correctness: false", "expression", "off"},
		{"preset: none, correctness: true", "expression", "on"},
		{"preset: none, correctness: all", "expression", "on"},
		{"correctness: {expression: true}", "expression", "on"},
		{"correctness: {expression: {level: true, options: null}}", "expression", "on"},
		{"correctness: {level: true}", "expression", "on"},
		{"correctness: {level: false}", "expression", "off"},
		{"correctness: {expression: false}", "expression", "off"},
		{"correctness: {expression: {level: false}}", "expression", "off"},
		{"correctness: null", "expression", "on"},
		{"correctness: {level: null}", "expression", "on"},
		{"correctness: {expression: null}", "expression", "on"},
		{"correctness: {expression: {level: null}}", "expression", "on"},
		{"correctness: {level: warn, expression: null}", "expression", "warn"},
		{"correctness: {level: warn, expression: {level: null}}", "expression", "warn"},
		{"preset: none, correctness: {expression: null}", "expression", "off"},
		{"suspicious: {case-insensitive-conditions: null}", "case-insensitive-conditions", "off"},
		{"correctness: default", "expression", "on"},
		{"correctness: {level: default}", "expression", "on"},
		{"correctness: {expression: default}", "expression", "on"},
		{"correctness: {expression: {level: default}}", "expression", "on"},
		{"correctness: {level: warn, expression: default}", "expression", "warn"},
		{"preset: none, correctness: {expression: default}", "expression", "off"},
		{"suspicious: {case-insensitive-conditions: default}", "case-insensitive-conditions", "off"},
	} {
		t.Run(tc.rules, func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte("lint: {rules: {" + tc.rules + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if got := repoconfig.DiagnosticLevel(repoconfig.ResolveRules(repoconfig.Presets{}, cfg), tc.name); got != tc.want {
					t.Fatalf("got %s, want %s", got, tc.want)
				}
				encoded, err := yaml.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				cfg, err = repoconfig.Parse(encoded)
				if err != nil {
					t.Fatalf("round trip: %s: %v", encoded, err)
				}
			}
		})
	}
	for _, rules := range []string{
		"correctness: ''", "correctness: {level: ''}",
		"correctness: {expression: ''}", "correctness: {expression: {level: ''}}",
	} {
		if _, err := repoconfig.Parse([]byte("lint: {rules: {" + rules + "}}")); err == nil {
			t.Fatalf("invalid setting accepted: %s", rules)
		}
	}
}

func TestRuleLevelAliasesWithStrict(t *testing.T) {
	for _, level := range []string{"off", "false", "default", "null"} {
		cfg, err := repoconfig.Parse([]byte("lint: {rules: {suspicious: {case-insensitive-conditions: " + level + "}}}"))
		if err != nil {
			t.Fatal(err)
		}
		want := repoconfig.Level("on")
		if level == "off" || level == "false" {
			want = "off"
		}
		if got := repoconfig.DiagnosticLevel(repoconfig.ResolveRules(repoconfig.Presets{Strict: true}, cfg), "case-insensitive-conditions"); got != want {
			t.Fatalf("%s: got %s, want %s", level, got, want)
		}
	}
}

func TestRuleLevelPrecedence(t *testing.T) {
	for _, tc := range []struct {
		rules  string
		preset repoconfig.Presets
		name   string
		want   repoconfig.Level
	}{
		{"preset: none", repoconfig.Presets{}, "expression", "off"},
		{"preset: none, correctness: {expression: warn}", repoconfig.Presets{}, "expression", "warn"},
		{"correctness: off", repoconfig.Presets{Strict: true}, "expression", "off"},
		{"correctness: {preset: none, expression: info}", repoconfig.Presets{Strict: true}, "expression", "info"},
		{"preset: all", repoconfig.Presets{}, "require-job-timeout", "on"},
		{"preset: all", repoconfig.Presets{}, "case-insensitive-conditions", "on"},
		{"suspicious: warn", repoconfig.Presets{}, "case-insensitive-conditions", "warn"},
		{"suspicious: warn", repoconfig.Presets{Experimental: new(true)}, "case-insensitive-conditions", "warn"},
		{"suspicious: off", repoconfig.Presets{Experimental: new(true)}, "case-insensitive-conditions", "off"},
		{"suspicious: {case-insensitive-conditions: info}", repoconfig.Presets{}, "case-insensitive-conditions", "info"},
		{"suspicious: {case-insensitive-conditions: info}", repoconfig.Presets{Experimental: new(false)}, "case-insensitive-conditions", "info"},
		{"policy: {preset: all, require-job-timeout: off}", repoconfig.Presets{}, "require-job-timeout", "off"},
		{"disable: [expression], correctness: error", repoconfig.Presets{Strict: true}, "expression", "off"},
	} {
		t.Run(tc.rules+"/"+tc.name+"/"+string(tc.want), func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte("lint: {rules: {" + tc.rules + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := repoconfig.DiagnosticLevel(repoconfig.ResolveRules(tc.preset, cfg), tc.name); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRuleSettingsInvalid(t *testing.T) {
	for _, rules := range []string{
		"preset: typo", "correctness: typo", "correctness: {expression: ''}",
		"correctness: {expression: warning}",
		"correctness: {expression: {options: {}}}", "correctness: {expression: {level: off, typo: 1}}",
		"correctness: {expression: {level: off, options: {}}}", "policy: {string-conditions: on}",
		"suspicious: {no-such-rule: on}", "policy: {require-job-timeout: {level: error, options: true}}",
		"policy: {require-job-timeout: {level: error, options: {max-minutes: -1}}}",
		"policy: {require-job-timeout: {level: error, options: {max-minute: 5}}}",
		"policy: {require-permissions: {level: error, options: {scope: step}}}",
		"policy: {required-actions: {level: error, options: {actions: ['[']}}}",
	} {
		t.Run(rules, func(t *testing.T) {
			if _, err := repoconfig.Parse([]byte("lint: {rules: {" + rules + "}}")); err == nil {
				t.Fatal("invalid settings accepted")
			}
		})
	}
}

func TestRuleSettingsOptionsAndRoundTrip(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`lint:
  rules:
    correctness: warn
    policy:
      require-job-timeout: {level: error, options: {min-minutes: 5, max-minutes: 30}}
      require-permissions: {level: warn, options: {scope: job}}
      required-actions: {level: error, options: {actions: ['actions/checkout@v*']}}
      disallow-suppressions: {level: error, options: {rules: [expression], report: violation}}
`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got := repoconfig.ResolveRules(repoconfig.Presets{}, cfg)
		if maximum, ok := got.RequiresJobTimeout().MaxMinutes(); !ok || maximum != 30 {
			t.Fatal(got.Policy)
		}
		if got.RequiresPermissions().Scope() != "job" || len(got.RequiredActions()) != 1 || got.Policy.DisallowSuppressions.Report() != "violation" {
			t.Fatal(got.Policy)
		}
		if repoconfig.DiagnosticLevel(got, "expression") != "warn" {
			t.Fatal(got.Lint)
		}
		encoded, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err = repoconfig.Parse(encoded)
		if err != nil {
			t.Fatalf("%s\n%v", encoded, err)
		}
	}
}

func TestTypedRuleOptionsNull(t *testing.T) {
	for rule, fields := range map[string][]string{
		"required-actions":      {"actions"},
		"require-job-timeout":   {"min-minutes", "max-minutes"},
		"require-permissions":   {"scope"},
		"disallow-suppressions": {"rules", "report"},
	} {
		t.Run(rule, func(t *testing.T) {
			options := []string{}
			for _, field := range fields {
				options = append(options, "{"+field+": null}")
			}
			for _, option := range options {
				text := fmt.Sprintf("lint: {rules: {policy: {%s: {level: on, options: %s}}}}", rule, option)
				if _, err := repoconfig.Parse([]byte(text)); err == nil {
					t.Fatalf("accepted invalid typed options: %s", text)
				}
			}
			for _, level := range []string{"null", "false", "default", "off"} {
				text := fmt.Sprintf("lint: {rules: {policy: {%s: %s}}}", rule, level)
				if _, err := repoconfig.Parse([]byte(text)); err != nil {
					t.Fatalf("level %s rejected: %v", level, err)
				}
			}
		})
	}
	for _, text := range []string{
		"policy: {required-actions: null}",
		"lint: {rules: {policy: {required-actions: {level: on, options: {actions: []}}}}}",
	} {
		cfg, err := repoconfig.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		if got := repoconfig.ResolveRules(repoconfig.Presets{}, cfg).RequiredActions(); len(got) != 0 {
			t.Fatalf("empty selection invented required actions: %v", got)
		}
	}
}
