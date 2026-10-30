package rulecatalog

import (
	"encoding/json"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
	"actionlint.kjanat.dev/v2/rule"
)

// Bundled provider rules come from their manifests and are configured in the
// external group; built-in rules never are.
func TestProviderRulesComeFromManifests(t *testing.T) {
	names := bundled.Names()
	if len(names) == 0 {
		t.Fatal("no bundled providers")
	}
	for _, name := range names {
		data, _ := bundled.Manifest(name)
		var manifest struct {
			Rule rule.Descriptor `json:"rule"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
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
