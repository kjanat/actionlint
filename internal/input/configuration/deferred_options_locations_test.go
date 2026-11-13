package config_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

func deferredOptionsRegistration() config.RuleRegistration {
	return config.RuleRegistration{
		Descriptor: rule.Descriptor{Name: "custom-check", Category: "security", Provider: "community", Maturity: "stable", OptionsSchema: json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1}},"additionalProperties":false}`)},
		Scope:      rule.SourceConfiguration,
	}
}

func TestDeferredPluginOptionsKeepInheritedSourceLocations(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, filename, source, path string
		line, column                 int
	}{
		{"root YAML", "base.yaml", "lint:\n  rules:\n    plugins:\n      custom-check:\n        level: on\n        options:\n          count: 0\n", "/lint/rules/plugins/custom-check/options/count", 7, 18},
		{"root TOML", "base.toml", "[lint.rules.plugins.custom-check]\nlevel = 'on'\n[lint.rules.plugins.custom-check.options]\ncount = 0\n", "/lint/rules/plugins/custom-check/options/count", 4, 9},
		{"file override", "base.yaml", "overrides:\n  - includes: ['*.yaml']\n    lint:\n      rules:\n        plugins:\n          custom-check:\n            level: on\n            options:\n              count: 0\n", "/overrides/0/lint/rules/plugins/custom-check/options/count", 9, 22},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			base := filepath.Join(dir, test.filename)
			if err := os.WriteFile(base, []byte(test.source), 0o600); err != nil {
				t.Fatal(err)
			}
			leaf := filepath.Join(dir, "leaf.jsonc")
			if err := os.WriteFile(leaf, []byte(`{"extends":["`+test.filename+`"],"lint":{"rules":{"plugins":{"custom-check":"off"}}}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			settings, err := checking.ConfigurationRules().Load(leaf, "custom-check")
			if err != nil {
				t.Fatal(err)
			}
			_, err = checking.ConfigurationRules().ResolveRegisteredRules(config.Presets{}, settings, deferredOptionsRegistration())
			assertNeutralSource(t, err, base, test.line, test.column, test.path)
		})
	}
}

func TestDeferredPluginOptionsKeepByteSourceLocations(t *testing.T) {
	t.Parallel()
	settings, err := checking.ConfigurationRules().Parse([]byte("lint:\n  rules:\n    plugins:\n      custom-check:\n        level: on\n        options:\n          count: 0\n"), "custom-check")
	if err != nil {
		t.Fatal(err)
	}
	_, err = checking.ConfigurationRules().ResolveRegisteredRules(config.Presets{}, settings, deferredOptionsRegistration())
	assertNeutralSource(t, err, "", 7, 18, "/lint/rules/plugins/custom-check/options/count")
}

func TestDeferredPluginOptionsKeepOverlaySourceLocations(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"config", "lint"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			source := "rules:\n  plugins:\n    custom-check:\n      level: on\n      options:\n        count: 0\n"
			line, column := 6, 16
			if input == "config" {
				source = "lint:\n  rules:\n    plugins:\n      custom-check:\n        level: on\n        options:\n          count: 0\n"
				line, column = 7, 18
			}
			overlay, err := checking.ConfigurationRules().ParseOverlayBeforePlugins(input, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			settings, _, err := checking.ConfigurationRules().Overlaid(nil, nil, []config.Overlay{overlay}, "custom-check")
			if err != nil {
				t.Fatal(err)
			}
			_, err = checking.ConfigurationRules().ResolveRegisteredRules(config.Presets{}, settings, deferredOptionsRegistration())
			assertNeutralSource(t, err, "", line, column, "/lint/rules/plugins/custom-check/options/count")
		})
	}
}

func TestCallerPluginOptionsAreNotAttributedToConfig(t *testing.T) {
	t.Parallel()
	settings, err := checking.ConfigurationRules().Parse([]byte("lint:\n  rules:\n    plugins:\n      custom-check: {level: on, options: {count: 2}}\n"), "custom-check")
	if err != nil {
		t.Fatal(err)
	}
	registration := deferredOptionsRegistration()
	registration.Options = json.RawMessage(`{"count":0}`)
	_, err = checking.ConfigurationRules().ResolveRegisteredRules(config.Presets{}, settings, registration)
	if err == nil {
		t.Fatal("invalid caller options accepted")
	}
	if location, ok := errors.AsType[*config.SourceError](err); ok {
		t.Fatalf("caller options were attributed to config: %v", location)
	}
	failure, ok := errors.AsType[*binding.Error](err)
	if !ok || failure.Path != "/count" {
		t.Fatalf("caller option path: %v", err)
	}
}
