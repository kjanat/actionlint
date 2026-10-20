package config_test

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestRuleGroupConfigErrors(t *testing.T) {
	for _, input := range []string{
		"lint: {rulse: {}}", "lint: {rules: {experimetnal: true}}",
		"lint: {rules: {experimental: true}}",
		"lint: {rules: {nursery: []}}",
		"lint: {rules: {nursery: {enabled: true}}}",
		"lint: {rules: {nursery: {preset: typo}}}",
		"lint: {rules: {nursery: {case-insensitive-conditions: on}}}",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := repoconfig.Parse([]byte(input)); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	for _, name := range []string{"string-conditions", "mixed-type-comparisons", "case-insensitive-conditions", "mixed-type-matrix-filters"} {
		if _, err := repoconfig.Parse([]byte("policy: {" + name + ": true}")); err == nil || !strings.Contains(err.Error(), "unknown key") {
			t.Fatalf("%s accepted as policy: %v", name, err)
		}
	}
}

func TestStableSuspiciousRuleSelection(t *testing.T) {
	for _, name := range []string{"string-conditions", "mixed-type-comparisons", "case-insensitive-conditions", "mixed-type-matrix-filters"} {
		descriptor, ok := repoconfig.FindRule(name)
		if !ok || descriptor.Category != "suspicious" || descriptor.Maturity == "nursery" || descriptor.Recommended {
			t.Fatalf("incorrect metadata: %+v", descriptor)
		}
		for _, tc := range []struct {
			rules   string
			presets repoconfig.Presets
			want    repoconfig.Level
		}{
			{"{}", repoconfig.Presets{}, "off"},
			{"{preset: recommended}", repoconfig.Presets{}, "off"},
			{"{preset: all}", repoconfig.Presets{}, "on"},
			{"{}", repoconfig.Presets{Strict: true}, "on"},
			{"{}", repoconfig.Presets{Experimental: new(true)}, "off"},
			{"{suspicious: {" + name + ": warn}}", repoconfig.Presets{}, "warn"},
			{"{suspicious: {" + name + ": warn}}", repoconfig.Presets{Experimental: new(false)}, "warn"},
			{"{suspicious: {" + name + ": off}}", repoconfig.Presets{Strict: true}, "off"},
			{"{suspicious: {preset: all}}", repoconfig.Presets{}, "on"},
			{"{suspicious: {preset: recommended}}", repoconfig.Presets{}, "off"},
		} {
			cfg, err := repoconfig.Parse([]byte("lint: {rules: " + tc.rules + "}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := repoconfig.DiagnosticLevel(repoconfig.ResolveRules(tc.presets, cfg), name); got != tc.want {
				t.Fatalf("%s %s: got %s, want %s", name, tc.rules, got, tc.want)
			}
		}
	}
}
