package main

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"testing"

	actionlint "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// The tools schema is generated from the bundled manifests: one entry per
// provider with its shorthands beside the host keys. Any other name may be a
// plugin's provider, so it gets the host keys only.
func TestToolsSchemaFollowsBundledManifests(t *testing.T) {
	t.Chdir("../..")
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(mapYAMLType(reflect.TypeFor[actionlint.ToolsConfig](), nil))
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	var tools struct {
		AdditionalProperties struct {
			OneOf []entry `json:"oneOf"`
		} `json:"additionalProperties"`
		Properties map[string]entry `json:"properties"`
	}
	if err := json.Unmarshal(data, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools.Properties) != len(manifests) {
		t.Fatalf("tools must list exactly the bundled providers: %s", data)
	}
	var other []string
	for _, alternative := range tools.AdditionalProperties.OneOf {
		for key := range alternative.Properties {
			other = append(other, key)
		}
	}
	slices.Sort(other)
	if !slices.Equal(other, []string{"backend", "config", "enabled"}) {
		t.Fatalf("other tools accept keys %v, want the host keys", other)
	}
	for _, manifest := range manifests {
		tool, ok := tools.Properties[manifest.Rule.Name]
		if !ok {
			t.Fatalf("missing tools.%s", manifest.Rule.Name)
		}
		var keys []string
		for key := range tool.Properties {
			keys = append(keys, key)
		}
		want := append([]string{"backend", "config", "enabled"}, slices.Collect(maps.Keys(manifest.ConfigShorthands))...)
		slices.Sort(keys)
		slices.Sort(want)
		if !slices.Equal(keys, want) {
			t.Fatalf("tools.%s keys = %v, want %v", manifest.Rule.Name, keys, want)
		}
	}
}
