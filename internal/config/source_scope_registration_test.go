package config

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestSourceScopeProjectionUsesRegistrations(t *testing.T) {
	root := t.TempDir()
	for _, descriptor := range Rules() {
		if descriptor.Scope != SourceScope {
			continue
		}
		groups := []string{descriptor.Category}
		if legacy := LegacyGroup(descriptor.Name); legacy != "" && legacy != descriptor.Category {
			groups = append(groups, legacy)
		}
		for _, group := range groups {
			for _, project := range []struct {
				name  string
				apply func(*Config, string, string) (*Config, error)
			}{{"action", ForAction}, {"suppressions", ForSuppressions}} {
				t.Run(descriptor.Name+"/"+group+"/"+project.name, func(t *testing.T) {
					input := fmt.Sprintf("lint: {rules: {preset: none, %s: {%s: warn}}}", group, descriptor.Name)
					cfg, err := Parse([]byte(input))
					if err != nil {
						t.Fatal(err)
					}
					projected, err := project.apply(cfg, filepath.Join(root, "action.yml"), root)
					if err != nil {
						t.Fatal(err)
					}
					resolved, err := ResolveRules(Presets{}, projected)
					if err != nil {
						t.Fatal(err)
					}
					if got := DiagnosticLevel(resolved, descriptor.Name); got != "warn" {
						t.Fatalf("source-scoped registration lost its explicit level: %s", got)
					}
				})
			}
		}
	}
}
