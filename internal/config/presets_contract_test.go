package config_test

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

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
