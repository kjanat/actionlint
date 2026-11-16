package configuration_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
)

func TestCapturedRuleLayersUseRegisteredProviderPrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		base, leaf string
		want       configuration.Level
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
			reads := 0
			loaded, err := checking.ConfigurationRules().Read(filepath.Join(directory, "leaf.yml"), func(path string) ([]byte, error) {
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
			registration := configuration.RuleRegistration{Descriptor: rule.Descriptor{Name: "shellcheck", Category: "correctness", Maturity: "stable", Recommended: true}, Scope: rule.SourceConfiguration, SupportsActions: true, ReplacesTool: "shellcheck"}
			rules, err := checking.ConfigurationRules().WithRegisteredRules(registration)
			if err != nil {
				t.Fatal(err)
			}
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
