package config_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestOverlayBeforePluginsDefersOnlyRuleNames(t *testing.T) {
	t.Parallel()
	overlay, err := checking.ConfigurationRules().ParseOverlayBeforePlugins("config", []byte("lint: {rules: {plugins: {example: warn}}}"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := checking.ConfigurationRules().Overlaid(nil, nil, []repoconfig.Overlay{overlay})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checking.ConfigurationRules().ResolveRules(repoconfig.Presets{}, cfg); err == nil {
		t.Fatal("unregistered rule survived final configuration binding")
	}
	if _, _, err := checking.ConfigurationRules().Overlaid(nil, nil, []repoconfig.Overlay{overlay}, "example"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"unknown-field: true",
		"plugins: [123]",
		"lint: {rules: {plugins: {example: invalid-level}}}",
		"tools: {ruff: {backend: {process: {}, wasm: {path: engine.wasm}}}}",
		"policy: {require-job-timeout: {min-minutes: 20, max-minutes: 10}}",
	} {
		if _, err := checking.ConfigurationRules().ParseOverlayBeforePlugins("config", []byte(invalid)); err == nil {
			t.Errorf("accepted invalid pre-plugin configuration: %s", invalid)
		}
	}
}
