package installation_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	installation "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestDefaultReferencesConsiderOverrides(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config string
		want         bool
	}{
		{"enabled override", "tools: {ruff: false}\noverrides: [{includes: ['python.yml'], tools: {ruff: true}}]", true},
		{"rule enabled override", "lint: {rules: {external: {ruff: off}}}\noverrides: [{includes: ['python.yml'], lint: {rules: {external: {ruff: on}}}}]", true},
		{"native override remains native", "tools: {ruff: false}\noverrides: [{includes: ['python.yml'], tools: {ruff: {enabled: true, backend: {process: {executable: ruff}}}}}]", false},
		{"explicit opt-out", "plugins: []\ntools: {ruff: false}\noverrides: [{includes: ['python.yml'], tools: {ruff: true}}]", false},
		{"always disabled", "tools: {ruff: false}\noverrides: [{includes: ['python.yml'], tools: {ruff: false}}]", false},
		{"overlapping overrides", "tools: {ruff: false}\noverrides: [{includes: ['**/*.yml'], tools: {ruff: true}}, {includes: ['python.yml'], tools: {ruff: {backend: {process: {executable: ruff}}}}}]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rules := checking.ConfigurationRules()
			cfg, err := rules.Parse([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			refs, err := installation.DefaultReferences(cfg, installation.DefaultPluginOptions{RuleSet: rules, Tools: []string{"ruff"}})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(refs) != 0; got != tc.want {
				t.Fatalf("selected = %v, want selection %t", refs, tc.want)
			}
		})
	}
}
