package config_test

import (
	"reflect"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"github.com/kjanat/go-yaml/v4"
)

func TestResolvedConfigRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"",
		`config-variables: []
config-secrets: null
policy: {cache-operation: false, disallow-suppressions: true}
`,
		`config-variables: null
config-secrets: []
policy: {cache-write-untrusted: null, require-job-timeout: {min-minutes: 5, max-minutes: 60}, require-permissions: {scope: job}, disallow-suppressions: {rules: [cache-operation], report: violation}}
paths: {'**': {ignore: ['a.*b']}}
`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			resolved, err := checking.ConfigurationRules().ResolveDocument([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			serialized, err := yaml.Marshal(resolved.Values)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip, err := checking.ConfigurationRules().ResolveDocument(serialized)
			if err != nil {
				t.Fatalf("effective configuration cannot be reused: %v\n%s", err, serialized)
			}
			if !reflect.DeepEqual(resolved.Values, roundTrip.Values) {
				t.Fatalf("effective configuration changed after roundtrip: %#v != %#v", resolved.Values, roundTrip.Values)
			}
			if (repoconfig.VariableNames(resolved.Config.Facts()) == nil) != (repoconfig.VariableNames(roundTrip.Config.Facts()) == nil) ||
				(repoconfig.SecretNames(resolved.Config.Facts()) == nil) != (repoconfig.SecretNames(roundTrip.Config.Facts()) == nil) {
				t.Fatal("serialization changed whether variable or secret checking is enabled")
			}
			if source == "" {
				tools := resolved.Values["tools"].(map[string]any)
				if selection, exists := tools["ruff"].(map[string]any)["select"]; exists {
					t.Fatalf("effective config overrides Ruff's defaults: %v", selection)
				}
				for _, rule := range []string{"cache-call-unrestricted", "cache-operation", "cache-write-untrusted"} {
					if !roundTrip.Config.CachePolicyEnabled(rule) || checking.ConfigurationRules().DiagnosticLevel(roundTrip.Config, rule) != "on" {
						t.Fatalf("lost cache policy default or origin: %s", rule)
					}
				}
			}
		})
	}
}
