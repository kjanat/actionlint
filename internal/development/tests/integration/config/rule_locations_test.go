package config_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestRuleLocationsMergeAcrossConfigSources(t *testing.T) {
	t.Parallel()
	rules := checking.ConfigurationRules()
	for _, categories := range [][2]string{{"policy", "conventions"}, {"conventions", "policy"}} {
		t.Run(categories[0]+"-to-"+categories[1], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			fixturefiles.Write(t, root, "base.yml", fmt.Sprintf("lint: {rules: {%s: {job-timeout: {level: error, options: {min-minutes: 5, max-minutes: 30}}}}}\n", categories[0]))
			path := fixturefiles.Write(t, root, "config.yml", fmt.Sprintf("extends: [base.yml]\nlint: {rules: {%s: {require-job-timeout: {level: warn, options: {max-minutes: 20}}}}}\n", categories[1]))
			loaded, err := rules.ReadContext(t.Context(), path, nil)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := rules.ResolveRules(repoconfig.Presets{}, loaded.Config)
			if err != nil {
				t.Fatal(err)
			}
			minimum, _ := selected.RequiresJobTimeout().MinMinutes()
			maximum, _ := selected.RequiresJobTimeout().MaxMinutes()
			if level := rules.DiagnosticLevel(selected, "job-timeout"); level != "warn" || minimum != 5 || maximum != 20 {
				t.Fatalf("inherited settings: level=%s minimum=%v maximum=%v", level, minimum, maximum)
			}
		})
	}
}
