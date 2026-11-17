package configuration_test

import (
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestRuleGroupConfigErrors(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"lint: {rulse: {}}", "lint: {rules: {experimetnal: true}}",
		"lint: {rules: {experimental: true}}",
		"lint: {rules: {nursery: []}}",
		"lint: {rules: {nursery: {enabled: true}}}",
		"lint: {rules: {nursery: {preset: typo}}}",
		"lint: {rules: {nursery: {case-insensitive-conditions: on}}}",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().Parse([]byte(input))
			if err == nil {
				_, err = checking.ConfigurationRules().ResolveRules(configuration.Presets{}, cfg)
			}
			if err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	for _, name := range []string{"string-conditions", "mixed-type-comparisons", "case-insensitive-conditions", "mixed-type-matrix-filters"} {
		if _, err := checking.ConfigurationRules().Parse([]byte("policy: {" + name + ": true}")); err == nil || !strings.Contains(err.Error(), "unknown key") {
			t.Fatalf("%s accepted as policy: %v", name, err)
		}
	}
}

func TestStableSuspiciousRuleSelection(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"string-conditions", "mixed-type-comparisons", "case-insensitive-conditions", "mixed-type-matrix-filters"} {
		descriptor, ok := checking.ConfigurationRules().FindRule(name)
		if !ok || descriptor.Category != "suspicious" || descriptor.Maturity == "nursery" || descriptor.Recommended {
			t.Fatalf("incorrect metadata: %+v", descriptor)
		}
		for _, tc := range []struct {
			rules   string
			presets configuration.Presets
			want    configuration.Level
		}{
			{"{}", configuration.Presets{}, "off"},
			{"{preset: recommended}", configuration.Presets{}, "off"},
			{"{preset: all}", configuration.Presets{}, "on"},
			{"{}", configuration.Presets{Strict: true}, "on"},
			{"{}", configuration.Presets{Experimental: new(true)}, "off"},
			{"{suspicious: {" + name + ": warn}}", configuration.Presets{}, "warn"},
			{"{suspicious: {" + name + ": warn}}", configuration.Presets{Experimental: new(false)}, "warn"},
			{"{suspicious: {" + name + ": off}}", configuration.Presets{Strict: true}, "off"},
			{"{suspicious: {preset: all}}", configuration.Presets{}, "on"},
			{"{suspicious: {preset: recommended}}", configuration.Presets{}, "off"},
		} {
			cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: " + tc.rules + "}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := checking.ConfigurationRules().DiagnosticLevel(resolveRules(t, tc.presets, cfg), name); got != tc.want {
				t.Fatalf("%s %s: got %s, want %s", name, tc.rules, got, tc.want)
			}
		}
	}
}
