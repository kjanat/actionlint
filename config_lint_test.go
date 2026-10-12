package actionlint

import (
	"strings"
	"testing"
)

func TestStableSuspiciousRuleSelection(t *testing.T) {
	for _, name := range []string{"string-conditions", "mixed-type-comparisons", "case-insensitive-conditions", "mixed-type-matrix-filters"} {
		descriptor, ok := findRuleDescriptor(name)
		if !ok || descriptor.Category != "suspicious" || descriptor.maturity() != "stable" || descriptor.Recommended {
			t.Fatalf("incorrect metadata: %+v", descriptor)
		}
		for _, tc := range []struct {
			rules   string
			presets RulePresets
			want    RuleLevel
		}{
			{"{}", RulePresets{}, "off"},
			{"{preset: recommended}", RulePresets{}, "off"},
			{"{preset: all}", RulePresets{}, "on"},
			{"{}", RulePresets{Strict: true}, "on"},
			{"{}", RulePresets{Experimental: new(true)}, "off"},
			{"{suspicious: {" + name + ": warn}}", RulePresets{}, "warn"},
			{"{suspicious: {" + name + ": warn}}", RulePresets{Experimental: new(false)}, "warn"},
			{"{suspicious: {" + name + ": off}}", RulePresets{Strict: true}, "off"},
			{"{suspicious: {preset: all}}", RulePresets{}, "on"},
			{"{suspicious: {preset: recommended}}", RulePresets{}, "off"},
		} {
			cfg, err := ParseConfig([]byte("lint: {rules: " + tc.rules + "}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := tc.presets.apply(cfg).diagnosticLevel(name); got != tc.want {
				t.Fatalf("%s %s: got %s, want %s", name, tc.rules, got, tc.want)
			}
		}
	}
}
func TestRuleGroupConfigErrors(t *testing.T) {
	for _, input := range []string{
		"lint: {rulse: {}}", "lint: {rules: {experimetnal: true}}",
		"lint: {rules: {experimental: true}}",
		"lint: {rules: {nursery: true}}", "lint: {rules: {nursery: []}}",
		"lint: {rules: {nursery: {enabled: true}}}",
		"lint: {rules: {nursery: {preset: typo}}}",
		"lint: {rules: {nursery: {case-insensitive-conditions: on}}}",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseConfig([]byte(input)); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	for _, name := range []string{"string-conditions", "mixed-type-comparisons", "case-insensitive-conditions", "mixed-type-matrix-filters"} {
		if _, err := ParseConfig([]byte("policy: {" + name + ": true}")); err == nil || !strings.Contains(err.Error(), "unknown key") {
			t.Fatalf("%s accepted as policy: %v", name, err)
		}
	}
}

func TestSuspiciousConditionChecks(t *testing.T) {
	for _, expression := range []string{
		"github.ref == 'refs/heads/main'", "github.actor != 'admin'",
		"startsWith(github.ref, 'refs/heads/')", "contains(github.event.label.name, 'approved')",
		"endsWith(github.event.deployment.environment, 'production')",
	} {
		for _, location := range []string{"job", "step", "snapshot"} {
			for _, config := range []string{"", "lint: {rules: {suspicious: off}}", "lint: {rules: {suspicious: {case-insensitive-conditions: on}}}"} {
				t.Run(expression+"/"+location+"/"+config, func(t *testing.T) {
					body := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
`
					switch location {
					case "job":
						body += "    if: " + expression + `
    steps:
      - run: echo ok
`
					case "step":
						body += `    steps:
      - run: echo ok
        if: ` + expression + "\n"
					case "snapshot":
						body += `    snapshot:
      image-name: test
      if: ` + expression + `
    steps:
      - run: echo ok
`
					}
					errs := lintCachePolicy(t, body, config)
					if strings.Contains(config, ": on") {
						if len(errs) != 1 || errs[0].Kind != "case-insensitive-conditions" {
							t.Fatal(errs)
						}
					} else if len(errs) != 0 {
						t.Fatal(errs)
					}
				})
			}
		}
	}
}
