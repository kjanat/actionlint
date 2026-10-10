package actionlint

import (
	"context"
	"io"
	"path/filepath"
	"testing"
)

func TestRulePresetsPreserveAbsentConfiguration(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n")
	for _, tc := range []struct {
		name    string
		config  *Config
		presets RulePresets
		wantNil bool
	}{
		{"default", nil, RulePresets{}, true},
		{"explicit configuration", &Config{}, RulePresets{}, false},
		{"strict", nil, RulePresets{Strict: true}, false},
		{"experimental enabled", nil, RulePresets{Experimental: new(true)}, false},
		{"experimental disabled", nil, RulePresets{Experimental: new(false)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := NewRuleBase("custom-config", "")
			called := false
			_, err := Analyze(context.Background(), AnalysisRequest{
				Sources:     []SourceUnit{{Path: "test.yml", Content: content, Config: tc.config}},
				WorkingDir:  t.TempDir(),
				RulePresets: tc.presets,
				OnRulesCreated: func(rules []Rule) []Rule {
					called = true
					return append(rules, &rule)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !called || (rule.Config() == nil) != tc.wantNil {
				t.Fatalf("custom rule config = %#v, want nil=%v; hook called=%v", rule.Config(), tc.wantNil, called)
			}
			if tc.presets.Strict && !rule.Config().RequiresCommitHash() {
				t.Fatal("strict preset was not applied")
			}
		})
	}
	t.Run("legacy linter", func(t *testing.T) {
		root := t.TempDir()
		rule := NewRuleBase("custom-config", "")
		called := false
		linter, err := NewLinter(io.Discard, &LinterOptions{
			WorkingDir: root,
			OnRulesCreated: func(rules []Rule) []Rule {
				called = true
				return append(rules, &rule)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := linter.Lint(filepath.Join(root, "test.yml"), content, nil); err != nil {
			t.Fatal(err)
		}
		if !called || rule.Config() != nil {
			t.Fatalf("custom rule config = %#v, want nil; hook called=%v", rule.Config(), called)
		}
	})
}

func TestNurserySelectionIsIndependent(t *testing.T) {
	// Exercise the lifecycle without misclassifying a shipping rule as nursery.
	descriptor := ruleDescriptor{Name: "under-development", Category: "nursery"}
	for _, tc := range []struct {
		rules   string
		presets RulePresets
		want    RuleLevel
	}{
		{"{}", RulePresets{}, "off"},
		{"{preset: all}", RulePresets{}, "off"},
		{"{}", RulePresets{Strict: true}, "off"},
		{"{}", RulePresets{Experimental: new(true)}, "on"},
		{"{nursery: warn}", RulePresets{}, "warn"},
		{"{nursery: {preset: all}}", RulePresets{}, "on"},
		{"{nursery: off}", RulePresets{Experimental: new(true)}, "off"},
		{"{nursery: warn}", RulePresets{Experimental: new(false)}, "off"},
	} {
		cfg, err := ParseConfig([]byte("lint: {rules: " + tc.rules + "}"))
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.ruleSetting(descriptor, tc.presets).Level; got != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.rules, got, tc.want)
		}
	}
	for _, info := range BuiltinRules() {
		if info.Category == "nursery" || info.Maturity != "stable" {
			t.Fatalf("unexpected nursery rule: %+v", info)
		}
	}
}

func TestStrictPreservesConfiguredPolicyOptions(t *testing.T) {
	cfg, err := ParseConfig([]byte(`policy:
  require-job-timeout: {min-minutes: 5, max-minutes: 30}
  require-permissions: {scope: job}
  disallow-suppressions: {rules: [if-cond], report: violation}
lint: {rules: {disable: [require-commit-hash], suspicious: {case-insensitive-conditions: off}}}
`))
	if err != nil {
		t.Fatal(err)
	}
	got := (RulePresets{Strict: true, Experimental: new(true)}).apply(cfg)
	if got.Policy.RequireJobTimeout != cfg.Policy.RequireJobTimeout || got.Policy.RequirePermissions != cfg.Policy.RequirePermissions || got.Policy.DisallowSuppressions != cfg.Policy.DisallowSuppressions {
		t.Fatal("preset replaced configured options")
	}
	if len(got.Lint.Rules.Disable) != 1 || got.diagnosticLevel("case-insensitive-conditions") != "off" || got.diagnosticLevel("string-conditions") == "off" {
		t.Fatal(got.Lint.Rules)
	}
	if cfg.RequiresCommitHash() || cfg.diagnosticLevel("string-conditions") != "off" {
		t.Fatal("preset mutated shared config")
	}
}
