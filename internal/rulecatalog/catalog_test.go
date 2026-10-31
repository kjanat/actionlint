package rulecatalog

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/rule"
)

// Bundled provider rules come from their manifests and are configured in the
// external group; built-in rules never are.
func TestProviderRulesComeFromManifests(t *testing.T) {
	manifests, err := provider.Bundled()
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
		found, ok := Find(name)
		if !ok || found.Description != manifest.Rule.Description || found.FixAvailability != manifest.Rule.FixAvailability {
			t.Fatalf("%s: catalog %+v, manifest %+v", name, found, manifest.Rule)
		}
		if !IsProvider(name) || ConfigGroup(name) != "external" {
			t.Fatalf("%s: provider=%v group=%q", name, IsProvider(name), ConfigGroup(name))
		}
	}
	for _, descriptor := range builtins {
		if IsProvider(descriptor.Name) || ConfigGroup(descriptor.Name) == "external" {
			t.Fatalf("built-in %s configured as a tool provider", descriptor.Name)
		}
	}
	if len(Rules()) != len(builtins)+len(names) {
		t.Fatalf("catalog has %d rules, want %d built-in and %d provider rules", len(Rules()), len(builtins), len(names))
	}
}
