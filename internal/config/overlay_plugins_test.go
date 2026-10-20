package config

import "testing"

func TestOverlayBeforePluginsDefersOnlyRuleNames(t *testing.T) {
	overlay, err := ParseOverlayBeforePlugins("config", []byte("lint: {rules: {plugins: {example: warn}}}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Overlaid(nil, nil, []Overlay{overlay}); err == nil {
		t.Fatal("unregistered rule survived final configuration binding")
	}
	if _, _, err := Overlaid(nil, nil, []Overlay{overlay}, "example"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"unknown-field: true",
		"plugins: [123]",
		"lint: {rules: {plugins: {example: invalid-level}}}",
		"tools: {ruff: {backend: {process: {}, wasm: {path: engine.wasm}}}}",
		"policy: {require-job-timeout: {min-minutes: 20, max-minutes: 10}}",
	} {
		if _, err := ParseOverlayBeforePlugins("config", []byte(invalid)); err == nil {
			t.Errorf("accepted invalid pre-plugin configuration: %s", invalid)
		}
	}
}
