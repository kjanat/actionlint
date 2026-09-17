package actionlint

import (
	"reflect"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestResolvedConfigRoundTrip(t *testing.T) {
	for _, source := range []string{
		"",
		"config-variables: []\nconfig-secrets: null\npolicy: {cache-operation: false, disallow-suppressions: true}\n",
		"config-variables: null\nconfig-secrets: []\npolicy: {cache-write-untrusted: null, require-job-timeout: {min-minutes: 5, max-minutes: 60}, require-permissions: {scope: job}, disallow-suppressions: {rules: [cache-operation], report: violation}}\npaths: {'**': {ignore: ['a.*b']}}\n",
	} {
		t.Run(source, func(t *testing.T) {
			resolved, err := resolveConfigDocument([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			serialized, err := yaml.Marshal(resolved.values)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip, err := resolveConfigDocument(serialized)
			if err != nil {
				t.Fatalf("effective configuration cannot be reused: %v\n%s", err, serialized)
			}
			if !reflect.DeepEqual(resolved.values, roundTrip.values) {
				t.Fatalf("effective configuration changed after roundtrip: %#v != %#v", resolved.values, roundTrip.values)
			}
			if (resolved.config.ConfigVariables == nil) != (roundTrip.config.ConfigVariables == nil) ||
				(resolved.config.ConfigSecrets == nil) != (roundTrip.config.ConfigSecrets == nil) {
				t.Fatal("serialization changed whether variable or secret checking is enabled")
			}
			if source == "" {
				for _, rule := range InlineSuppressibleRules() {
					if !roundTrip.config.cachePolicyEnabled(rule) || resolved.origins["/policy/"+rule].Source != "default" {
						t.Fatalf("lost cache policy default or origin: %s", rule)
					}
				}
			}
		})
	}
}
