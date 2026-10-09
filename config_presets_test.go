package actionlint

import "testing"

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
  disallow-suppressions: {rules: [cache-operation], report: violation}
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
