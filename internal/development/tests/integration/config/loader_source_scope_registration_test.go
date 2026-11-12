package config_test

import (
	"fmt"
	"path/filepath"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	rulescope "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestSourceScopeProjectionUsesRegistrations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, descriptor := range checking.ConfigurationRules().Rules() {
		if descriptor.Scope != rulescope.SourceConfiguration {
			continue
		}
		groups := []string{descriptor.Category}
		if legacy := checking.ConfigurationRules().LegacyGroup(descriptor.Name); legacy != "" && legacy != descriptor.Category {
			groups = append(groups, legacy)
		}
		for _, group := range groups {
			for _, project := range []struct {
				name  string
				apply func(*repoconfig.Config, string, string) (*repoconfig.Config, error)
			}{{"action", checking.ConfigurationRules().ForAction}, {"suppressions", checking.ConfigurationRules().ForSuppressions}} {
				t.Run(descriptor.Name+"/"+group+"/"+project.name, func(t *testing.T) {
					t.Parallel()
					input := fmt.Sprintf("lint: {rules: {preset: none, %s: {%s: warn}}}", group, descriptor.Name)
					cfg, err := checking.ConfigurationRules().Parse([]byte(input))
					if err != nil {
						t.Fatal(err)
					}
					projected, err := project.apply(cfg, filepath.Join(root, "action.yml"), root)
					if err != nil {
						t.Fatal(err)
					}
					resolved, err := checking.ConfigurationRules().ResolveRules(repoconfig.Presets{}, projected)
					if err != nil {
						t.Fatal(err)
					}
					if got := checking.ConfigurationRules().DiagnosticLevel(resolved, descriptor.Name); got != "warn" {
						t.Fatalf("source-scoped registration lost its explicit level: %s", got)
					}
				})
			}
		}
	}
}
