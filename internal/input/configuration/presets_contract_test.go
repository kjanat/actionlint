package config_test

import (
	"reflect"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
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
		cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: " + tc.rules + "}"))
		if err != nil {
			t.Fatal(err)
		}
		if got := checking.ConfigurationRules().SettingFor(cfg, descriptor, tc.presets, false).Level(); got != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.rules, got, tc.want)
		}
	}
	for _, entry := range checking.ConfigurationRules().Rules() {
		if !entry.Experimental() {
			continue
		}
		for _, tc := range []struct {
			rules   string
			presets repoconfig.Presets
		}{
			{"{}", repoconfig.Presets{}},
			{"{preset: all}", repoconfig.Presets{}},
			{"{}", repoconfig.Presets{Strict: true}},
		} {
			cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: " + tc.rules + "}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := checking.ConfigurationRules().SettingFor(cfg, entry, tc.presets, false).Level(); got != "off" {
				t.Errorf("%s with %s and presets %+v: got %s, want off", entry.Name, tc.rules, tc.presets, got)
			}
		}
	}
}

func TestStrictPreservesConfiguredPolicyOptions(t *testing.T) {
	cfg, err := checking.ConfigurationRules().Parse([]byte(`policy:
  require-job-timeout: {min-minutes: 5, max-minutes: 30}
  require-permissions: {scope: job}
  disallow-suppressions: {rules: [if-cond], report: violation}
lint: {rules: {disable: [require-commit-hash], suspicious: {case-insensitive-conditions: off}}}
`))
	if err != nil {
		t.Fatal(err)
	}
	got := resolveRules(t, repoconfig.Presets{Strict: true, Experimental: new(true)}, cfg)
	// Each resolution decodes its own copy of the options, so compare values.
	if !reflect.DeepEqual(got.Policy.RequireJobTimeout, cfg.Policy.RequireJobTimeout) || !reflect.DeepEqual(got.Policy.RequirePermissions, cfg.Policy.RequirePermissions) || !reflect.DeepEqual(got.Policy.DisallowSuppressions, cfg.Policy.DisallowSuppressions) {
		t.Fatal("preset replaced configured options")
	}
	if maximum, ok := got.RequiresJobTimeout().MaxMinutes(); !ok || maximum != 30 || got.RequiresPermissions().Scope() != "job" || got.Policy.DisallowSuppressions.Report() != "violation" {
		t.Fatal("configured options lost")
	}
	if len(got.Lint.Rules.Disable) != 1 || checking.ConfigurationRules().DiagnosticLevel(got, "case-insensitive-conditions") != "off" || checking.ConfigurationRules().DiagnosticLevel(got, "string-conditions") == "off" {
		t.Fatal(got.Lint.Rules)
	}
	if cfg.RequiresCommitHash() || checking.ConfigurationRules().DiagnosticLevel(cfg, "string-conditions") != "off" {
		t.Fatal("preset mutated shared config")
	}
}
