package actionlint

import (
	"fmt"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestCatalogRuleConfiguration(t *testing.T) {
	for _, d := range builtinRuleDescriptors() {
		t.Run(d.Name, func(t *testing.T) {
			cfg, err := ParseConfig(fmt.Appendf(nil, "lint: {rules: {%s: {%s: off}}}", d.Category, d.Name))
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []RulePresets{{}, {Strict: true, Experimental: new(true)}} {
				if got := p.apply(cfg).diagnosticLevel(d.Name); got != "off" {
					t.Fatalf("explicit off overridden: %s", got)
				}
			}
			if cfg.Lint.Rules.resolved != nil {
				t.Fatal("shared config mutated")
			}
			if d.isNursery() && d.Recommended {
				t.Fatal("nursery rule marked recommended")
			}
		})
	}
}

func TestRuleLevelPrecedence(t *testing.T) {
	for _, tc := range []struct {
		rules  string
		preset RulePresets
		name   string
		want   RuleLevel
	}{
		{"preset: none", RulePresets{}, "expression", "off"},
		{"preset: none, correctness: {expression: warn}", RulePresets{}, "expression", "warn"},
		{"correctness: off", RulePresets{Strict: true}, "expression", "off"},
		{"correctness: {preset: none, expression: info}", RulePresets{Strict: true}, "expression", "info"},
		{"preset: all", RulePresets{}, "require-job-timeout", "on"},
		{"preset: all", RulePresets{}, "case-insensitive-conditions", "on"},
		{"suspicious: warn", RulePresets{}, "case-insensitive-conditions", "warn"},
		{"suspicious: warn", RulePresets{Experimental: new(true)}, "case-insensitive-conditions", "warn"},
		{"suspicious: off", RulePresets{Experimental: new(true)}, "case-insensitive-conditions", "off"},
		{"suspicious: {case-insensitive-conditions: info}", RulePresets{}, "case-insensitive-conditions", "info"},
		{"suspicious: {case-insensitive-conditions: info}", RulePresets{Experimental: new(false)}, "case-insensitive-conditions", "info"},
		{"policy: {preset: all, require-job-timeout: off}", RulePresets{}, "require-job-timeout", "off"},
		{"disable: [expression], correctness: error", RulePresets{Strict: true}, "expression", "off"},
	} {
		t.Run(tc.rules+"/"+tc.name+"/"+string(tc.want), func(t *testing.T) {
			cfg, err := ParseConfig([]byte("lint: {rules: {" + tc.rules + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := tc.preset.apply(cfg).diagnosticLevel(tc.name); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRuleSettingsInvalid(t *testing.T) {
	for _, rules := range []string{
		"preset: typo", "correctness: true", "correctness: typo", "correctness: {expression: ''}",
		"correctness: {expression: true}", "correctness: {expression: warning}",
		"correctness: {expression: {options: {}}}", "correctness: {expression: {level: off, typo: 1}}",
		"correctness: {expression: {level: off, options: {}}}", "policy: {string-conditions: on}",
		"suspicious: {no-such-rule: on}", "policy: {require-job-timeout: {level: error, options: true}}",
		"policy: {require-job-timeout: {level: error, options: {max-minutes: -1}}}",
		"policy: {require-job-timeout: {level: error, options: {max-minute: 5}}}",
		"policy: {require-permissions: {level: error, options: {scope: step}}}",
		"policy: {required-actions: {level: error, options: {actions: ['[']}}}",
	} {
		t.Run(rules, func(t *testing.T) {
			if _, err := ParseConfig([]byte("lint: {rules: {" + rules + "}}")); err == nil {
				t.Fatal("invalid settings accepted")
			}
		})
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
			options := []string{"null"}
			for _, field := range fields {
				options = append(options, "{"+field+": null}")
			}
			for _, option := range options {
				text := fmt.Sprintf("lint: {rules: {policy: {%s: {level: on, options: %s}}}}", rule, option)
				if _, err := ParseConfig([]byte(text)); err == nil {
					t.Fatalf("accepted invalid typed options: %s", text)
				}
			}
			for _, level := range []string{"null", "false", "default", "off"} {
				text := fmt.Sprintf("lint: {rules: {policy: {%s: %s}}}", rule, level)
				if _, err := ParseConfig([]byte(text)); err != nil {
					t.Fatalf("level %s rejected: %v", level, err)
				}
			}
		})
	}
	for _, text := range []string{
		"policy: {required-actions: null}",
		"lint: {rules: {policy: {required-actions: {level: on, options: {actions: []}}}}}",
	} {
		cfg, err := ParseConfig([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		if got := (RulePresets{}).apply(cfg).RequiredActions(); len(got) != 0 {
			t.Fatalf("empty selection invented required actions: %v", got)
		}
	}
}

func TestRuleLevelAliasesAndDefaults(t *testing.T) {
	for _, tc := range []struct {
		rules string
		name  string
		want  RuleLevel
	}{
		{"correctness: false", "expression", "off"},
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
			cfg, err := ParseConfig([]byte("lint: {rules: {" + tc.rules + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if got := (RulePresets{}).apply(cfg).diagnosticLevel(tc.name); got != tc.want {
					t.Fatalf("got %s, want %s", got, tc.want)
				}
				encoded, err := yaml.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				cfg, err = ParseConfig(encoded)
				if err != nil {
					t.Fatalf("round trip: %s: %v", encoded, err)
				}
			}
		})
	}
	for _, rules := range []string{
		"correctness: ''", "correctness: {level: ''}",
		"correctness: {expression: ''}", "correctness: {expression: {level: ''}}",
		"correctness: {expression: {level: true}}", "correctness: {level: true}",
	} {
		if _, err := ParseConfig([]byte("lint: {rules: {" + rules + "}}")); err == nil {
			t.Fatalf("invalid setting accepted: %s", rules)
		}
	}
}

func TestRuleLevelAliasesWithStrict(t *testing.T) {
	for _, level := range []string{"off", "false", "default", "null"} {
		cfg, err := ParseConfig([]byte("lint: {rules: {suspicious: {case-insensitive-conditions: " + level + "}}}"))
		if err != nil {
			t.Fatal(err)
		}
		want := RuleLevel("on")
		if level == "off" || level == "false" {
			want = "off"
		}
		if got := (RulePresets{Strict: true}).apply(cfg).diagnosticLevel("case-insensitive-conditions"); got != want {
			t.Fatalf("%s: got %s, want %s", level, got, want)
		}
	}
}

func TestRuleSettingsOptionsAndRoundTrip(t *testing.T) {
	cfg, err := ParseConfig([]byte(`lint:
  rules:
    correctness: warn
    policy:
      require-job-timeout: {level: error, options: {min-minutes: 5, max-minutes: 30}}
      require-permissions: {level: warn, options: {scope: job}}
      required-actions: {level: error, options: {actions: ['actions/checkout@v*']}}
      disallow-suppressions: {level: error, options: {rules: [cache-operation], report: violation}}
`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got := (RulePresets{}).apply(cfg)
		if maximum, ok := got.RequiresJobTimeout().MaxMinutes(); !ok || maximum != 30 {
			t.Fatal(got.Policy)
		}
		if got.RequiresPermissions().Scope() != "job" || len(got.RequiredActions()) != 1 || got.Policy.DisallowSuppressions.Report() != "violation" {
			t.Fatal(got.Policy)
		}
		if got.diagnosticLevel("expression") != "warn" {
			t.Fatal(got.Lint)
		}
		encoded, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err = ParseConfig(encoded)
		if err != nil {
			t.Fatalf("%s\n%v", encoded, err)
		}
	}
}

func TestRuleSettingsAnalysis(t *testing.T) {
	const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    if: true\n    steps:\n      - run: echo ok\n"
	for _, level := range []string{"off", "on", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			findings := lintCachePolicy(t, source, "lint: {rules: {correctness: {if-cond: "+level+"}}}")
			if level == "off" {
				if len(findings) != 0 {
					t.Fatal(findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Kind != "if-cond" {
				t.Fatal(findings)
			}
			want := level
			if level == "on" {
				want = ""
			}
			if level == "warn" {
				want = "warning"
			}
			if findings[0].severity != want {
				t.Fatalf("got %q, want %q", findings[0].severity, want)
			}
		})
	}
	if got := lintCachePolicy(t, source, "lint: {enabled: false}"); len(got) != 0 {
		t.Fatal(got)
	}
	findings := lintCachePolicy(t, source, "lint: {rules: {correctness: {if-cond: off}, policy: {require-job-timeout: {level: warn, options: {max-minutes: 5}}}}}")
	if len(findings) != 1 || findings[0].Kind != "require-job-timeout" || findings[0].severity != "warning" {
		t.Fatal(findings)
	}
}
