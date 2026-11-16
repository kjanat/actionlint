package configuration_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/ruleconfig"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// Rule settings are normalized per file with the rule set that loads them,
// before inheritance merges the files. A configuration loaded with a plugin
// provider's registration therefore orders its native and plugin spellings
// by layer.
func TestRuleLayersUseRegisteredProviderPrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		base, leaf string
		want       ruleconfig.Level
	}{
		{"same layer", "", "lint: {rules: {external: {shellcheck: off}, plugins: {shellcheck: on}}}", "on"},
		{"inherited native", "lint: {rules: {external: {shellcheck: off}}}", "extends: [base.yml]\nlint: {rules: {plugins: {shellcheck: on}}}", "on"},
		{"inherited plugin", "lint: {rules: {plugins: {shellcheck: off}}}", "extends: [base.yml]\nlint: {rules: {external: {shellcheck: on}}}", "on"},
		{"file override", "", "lint: {rules: {external: {shellcheck: off}, plugins: {shellcheck: off}}}\noverrides: [{includes: [ci.yml], lint: {rules: {plugins: {shellcheck: on}}}}]", "on"},
		{"category reset", "lint: {rules: {external: {shellcheck: off}}}", "extends: [base.yml]\nlint: {rules: {correctness: null}}", "on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			registration := configuration.RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "shellcheck", Category: "correctness", Maturity: "stable", Recommended: true}, Scope: rule.SourceConfiguration, SupportsActions: true}, ReplacesTool: "shellcheck"}
			rules, err := checking.ConfigurationRules().WithRegisteredRules(registration)
			if err != nil {
				t.Fatal(err)
			}
			reads := 0
			loaded, err := rules.Read(filepath.Join(directory, "leaf.yml"), func(path string) ([]byte, error) {
				reads++
				switch filepath.Base(path) {
				case "leaf.yml":
					return []byte(tc.leaf), nil
				case "base.yml":
					return []byte(tc.base), nil
				default:
					return nil, os.ErrNotExist
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			capturedReads := reads
			for range 2 {
				selected, err := rules.ForFile(loaded.Config, "ci.yml", directory)
				if err != nil {
					t.Fatal(err)
				}
				resolved, err := checking.ConfigurationRules().ResolveRegisteredRules(configuration.Presets{}, selected, registration)
				if err != nil {
					t.Fatal(err)
				}
				if got := resolved.RuleSet().DiagnosticLevel(resolved, "shellcheck"); got != tc.want {
					t.Fatalf("level=%s, want %s", got, tc.want)
				}
			}
			if reads != capturedReads {
				t.Fatalf("rule resolution read configuration again: %d -> %d", capturedReads, reads)
			}
		})
	}
}

// The run's configuration is resolved once, with the final rule set: the load
// normalizes each file's rule settings, and Overlaid normalizes an overlay
// parsed before plugins loaded. Selecting a file's settings afterwards
// returns that resolved value without merging rule settings again.
func TestConfigurationResolvedOnceWithFinalRules(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	files := map[string]string{
		"base.yml": "lint: {rules: {external: {shellcheck: off}}}",
		"leaf.yml": "extends: [base.yml]\nlint: {rules: {plugins: {shellcheck: on}}}",
		"flat.yml": "lint: {rules: {external: {shellcheck: off}}}",
	}
	read := func(path string) ([]byte, error) {
		content, ok := files[filepath.Base(path)]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	}
	registration := configuration.RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "shellcheck", Category: "correctness", Maturity: "stable", Recommended: true}, Scope: rule.SourceConfiguration, SupportsActions: true}, ReplacesTool: "shellcheck"}
	rules, err := checking.ConfigurationRules().WithRegisteredRules(registration)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := checking.ConfigurationRules().ParseOverlayBeforePlugins("config", []byte("lint: {rules: {plugins: {shellcheck: on}}}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		file     string
		overlays []configuration.Overlay
	}{
		{"inherited plugin spelling", "leaf.yml", nil},
		{"overlay parsed before plugins", "flat.yml", []configuration.Overlay{overlay}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loaded, err := rules.Read(filepath.Join(directory, tc.file), read)
			if err != nil {
				t.Fatal(err)
			}
			config, _, err := rules.Overlaid(loaded.Config, loaded, tc.overlays)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := rules.ForFile(config, "ci.yml", directory)
			if err != nil {
				t.Fatal(err)
			}
			if selected != config {
				t.Fatal("selecting a file's settings resolved the configuration again")
			}
			resolved, err := rules.ResolveSettings(configuration.Presets{}, selected)
			if err != nil {
				t.Fatal(err)
			}
			if got := resolved.RuleSet().DiagnosticLevel(resolved, "shellcheck"); got != "on" {
				t.Fatalf("level=%s, want on", got)
			}
		})
	}
}
