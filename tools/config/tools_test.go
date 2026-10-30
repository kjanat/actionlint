package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	actionlint "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// The tools schema is generated from the bundled manifests: one entry per
// provider, its shorthands beside the host keys, and no other names.
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
	var tools struct {
		AdditionalProperties bool `json:"additionalProperties"`
		Properties           map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &tools); err != nil {
		t.Fatal(err)
	}
	if tools.AdditionalProperties || len(tools.Properties) != len(manifests) {
		t.Fatalf("tools must list exactly the bundled providers: %s", data)
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
		want := append([]string{"backend", "config", "enabled"}, manifest.ConfigShorthands...)
		slices.Sort(keys)
		slices.Sort(want)
		if !slices.Equal(keys, want) {
			t.Fatalf("tools.%s keys = %v, want %v", manifest.Rule.Name, keys, want)
		}
	}
}
