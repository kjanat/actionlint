package checking

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

func TestBundledRuleCatalogLoads(t *testing.T) {
	t.Parallel()
	rules, err := LoadConfigurationRules()
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if _, declared := rules.FindRule(manifest.Rule.Name); !declared {
			t.Errorf("bundled provider %q has no rule declaration", manifest.Rule.Name)
		}
		if _, declared := ConfigurationRules().FindRule(manifest.Rule.Name); !declared {
			t.Errorf("ConfigurationRules omits bundled provider %q", manifest.Rule.Name)
		}
	}
}
