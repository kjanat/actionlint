package config_test

import (
	"bytes"
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestFileOverridesMergeAcceptedRuleLocations(t *testing.T) {
	t.Parallel()
	rules := checking.ConfigurationRules()
	for _, descriptor := range rules.Rules() {
		legacy := rules.LegacyGroup(descriptor.Name)
		if legacy == "" || legacy == descriptor.Category {
			continue
		}
		names := []string{descriptor.Name}
		for _, alias := range descriptor.Aliases {
			names = append(names, alias.Name)
		}
		for _, name := range names {
			for _, categories := range [][2]string{{legacy, descriptor.Category}, {descriptor.Category, legacy}} {
				t.Run(name+"/"+categories[0]+"-to-"+categories[1], func(t *testing.T) {
					t.Parallel()
					input := fmt.Appendf(nil, "lint: {rules: {%s: {%s: warn}}}\noverrides:\n  - includes: ['target.yml']\n    lint: {rules: {%s: {%s: off}}}\n", categories[0], name, categories[1], name)
					project, err := rules.Parse(input)
					if err != nil {
						t.Fatal(err)
					}
					before, err := rules.Marshal(project)
					if err != nil {
						t.Fatal(err)
					}
					for range 2 {
						selected, err := rules.ForFile(project, "target.yml", "")
						if err != nil {
							t.Fatal(err)
						}
						selected = resolveRules(t, repoconfig.Presets{Strict: true}, selected)
						if got := rules.DiagnosticLevel(selected, descriptor.Name); got != "off" {
							t.Fatalf("later setting lost to category order: %s", got)
						}
					}
					after, err := rules.Marshal(project)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatalf("file projection changed the project: %v\n%s", err, after)
					}
				})
			}
		}
	}
}

func TestFileOverridePreservesSameLayerRulePrecedence(t *testing.T) {
	t.Parallel()
	rules := checking.ConfigurationRules()
	project, err := rules.Parse([]byte(`lint:
  rules:
    policy:
      require-job-timeout: {level: error, options: {min-minutes: 5, max-minutes: 30}}
    conventions:
      job-timeout: {level: default, options: {max-minutes: 20}}
overrides:
  - includes: ['target.yml']
    lint: {rules: {correctness: {expression-type: on}}}
  - includes: ['options.yml']
    lint:
      rules:
        policy:
          require-job-timeout: {level: warn, options: {min-minutes: 10}}
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path    string
		level   repoconfig.Level
		minimum float64
	}{{"target.yml", "error", 0}, {"options.yml", "warn", 10}} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			selected, err := rules.ForFile(project, tc.path, "")
			if err != nil {
				t.Fatal(err)
			}
			selected = resolveRules(t, repoconfig.Presets{}, selected)
			minimum, _ := selected.RequiresJobTimeout().MinMinutes()
			maximum, _ := selected.RequiresJobTimeout().MaxMinutes()
			if rules.DiagnosticLevel(selected, "job-timeout") != tc.level || minimum != tc.minimum || maximum != 20 {
				t.Fatalf("level=%s minimum=%v maximum=%v", rules.DiagnosticLevel(selected, "job-timeout"), minimum, maximum)
			}
		})
	}
}
