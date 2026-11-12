package checking

import (
	"reflect"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	pluginmanifests "actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// Bundled provider rules use the semantic categories declared by their manifests.
func TestProviderRulesComeFromManifests(t *testing.T) {
	t.Parallel()
	manifests, err := pluginmanifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) == 0 {
		t.Fatal("no bundled providers")
	}
	var names []string
	var rules []rule.Descriptor
	for _, manifest := range manifests {
		names = append(names, manifest.Rule.Name)
		rules = append(rules, manifest.Rule)
	}
	if !reflect.DeepEqual(providerRules(), rules) {
		t.Fatalf("catalog provider rules %+v, validated manifests %+v", providerRules(), rules)
	}
	for _, manifest := range manifests {
		name := manifest.Rule.Name
		found, ok := FindRule(name)
		if !ok || !reflect.DeepEqual(found, manifest.Rule) {
			t.Fatalf("%s: catalog %+v, manifest %+v", name, found, manifest.Rule)
		}
		if !IsProviderRule(name) || RuleCategory(name) != manifest.Rule.Category {
			t.Fatalf("%s: provider=%v group=%q", name, IsProviderRule(name), RuleCategory(name))
		}
	}
	for _, descriptor := range builtins {
		if IsProviderRule(descriptor.Name) || RuleCategory(descriptor.Name) != descriptor.Category || !rule.ValidCategory(descriptor.Category) {
			t.Fatalf("built-in %s configured as a tool provider", descriptor.Name)
		}
	}
	if len(ListRules()) != len(builtins)+len(names) {
		t.Fatalf("catalog has %d rules, want %d built-in and %d provider rules", len(ListRules()), len(builtins), len(names))
	}
}
