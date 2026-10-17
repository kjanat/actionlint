package engine

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	builtin "actionlint.kjanat.dev/internal/builtin"

	repoconfig "actionlint.kjanat.dev/internal/config"

	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

func TestRulePresetsPreserveAbsentConfiguration(t *testing.T) {
	content := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`)
	for _, tc := range []struct {
		name    string
		config  *repoconfig.Config
		presets repoconfig.Presets
		wantNil bool
	}{
		{"default", nil, repoconfig.Presets{}, true},
		{"explicit configuration", &repoconfig.Config{}, repoconfig.Presets{}, false},
		{"strict", nil, repoconfig.Presets{Strict: true}, false},
		{"experimental enabled", nil, repoconfig.Presets{Experimental: new(true)}, false},
		{"experimental disabled", nil, repoconfig.Presets{Experimental: new(false)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := struct {
				workflowvisit.Base
				builtin.Configuration
			}{Base: workflowvisit.NewBase("custom-config", "")}
			called := false
			_, err := Analyze(context.Background(), AnalysisRequest{
				Sources:     []SourceUnit{{Path: "test.yml", Content: content, Config: tc.config}},
				WorkingDir:  t.TempDir(),
				RulePresets: tc.presets,
				OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
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
		rule := struct {
			workflowvisit.Base
			builtin.Configuration
		}{Base: workflowvisit.NewBase("custom-config", "")}
		called := false
		linter, err := NewLinter(io.Discard, &LinterOptions{
			WorkingDir: root,
			OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
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
	descriptor := repoconfig.Rule{Name: "under-development", Category: "correctness", Maturity: "nursery", Group: "nursery"}
	for _, tc := range []struct {
		rules   string
		presets repoconfig.Presets
		want    repoconfig.Level
	}{
		{"{}", repoconfig.Presets{}, "off"},
		{"{preset: all}", repoconfig.Presets{}, "off"},
		{"{}", repoconfig.Presets{Strict: true}, "off"},
		{"{}", repoconfig.Presets{Experimental: new(true)}, "on"},
		{"{nursery: warn}", repoconfig.Presets{}, "warn"},
		{"{nursery: {preset: all}}", repoconfig.Presets{}, "on"},
		{"{nursery: off}", repoconfig.Presets{Experimental: new(true)}, "off"},
		{"{nursery: warn}", repoconfig.Presets{Experimental: new(false)}, "off"},
	} {
		cfg, err := repoconfig.Parse([]byte("lint: {rules: " + tc.rules + "}"))
		if err != nil {
			t.Fatal(err)
		}
		if got := repoconfig.SettingFor(cfg, descriptor, tc.presets, false).Level; got != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.rules, got, tc.want)
		}
	}
	for _, info := range repoconfig.RuleInfos() {
		if info.Category == "nursery" || info.Maturity != "stable" {
			t.Fatalf("unexpected nursery rule: %+v", info)
		}
	}
}

func TestStrictPreservesConfiguredPolicyOptions(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`policy:
  require-job-timeout: {min-minutes: 5, max-minutes: 30}
  require-permissions: {scope: job}
  disallow-suppressions: {rules: [if-cond], report: violation}
lint: {rules: {disable: [require-commit-hash], suspicious: {case-insensitive-conditions: off}}}
`))
	if err != nil {
		t.Fatal(err)
	}
	got := repoconfig.ResolveRules(repoconfig.Presets{Strict: true, Experimental: new(true)}, cfg)
	if got.Policy.RequireJobTimeout != cfg.Policy.RequireJobTimeout || got.Policy.RequirePermissions != cfg.Policy.RequirePermissions || got.Policy.DisallowSuppressions != cfg.Policy.DisallowSuppressions {
		t.Fatal("preset replaced configured options")
	}
	if len(got.Lint.Rules.Disable) != 1 || repoconfig.DiagnosticLevel(got, "case-insensitive-conditions") != "off" || repoconfig.DiagnosticLevel(got, "string-conditions") == "off" {
		t.Fatal(got.Lint.Rules)
	}
	if cfg.RequiresCommitHash() || repoconfig.DiagnosticLevel(cfg, "string-conditions") != "off" {
		t.Fatal("preset mutated shared config")
	}
}
