package repoconfig

import (
	"reflect"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestResolvedConfigRoundTrip(t *testing.T) {
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
			resolved, err := ResolveDocument([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			serialized, err := yaml.Marshal(resolved.Values)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip, err := ResolveDocument(serialized)
			if err != nil {
				t.Fatalf("effective configuration cannot be reused: %v\n%s", err, serialized)
			}
			if !reflect.DeepEqual(resolved.Values, roundTrip.Values) {
				t.Fatalf("effective configuration changed after roundtrip: %#v != %#v", resolved.Values, roundTrip.Values)
			}
			if (resolved.Config.ConfigVariables == nil) != (roundTrip.Config.ConfigVariables == nil) ||
				(resolved.Config.ConfigSecrets == nil) != (roundTrip.Config.ConfigSecrets == nil) {
				t.Fatal("serialization changed whether variable or secret checking is enabled")
			}
			if source == "" {
				tools := resolved.Values["tools"].(map[string]any)
				if selection, exists := tools["ruff"].(map[string]any)["select"]; exists {
					t.Fatalf("effective config overrides Ruff's defaults: %v", selection)
				}
				for _, rule := range []string{"cache-call-unrestricted", "cache-operation", "cache-write-untrusted"} {
					if !roundTrip.Config.CachePolicyEnabled(rule) || resolved.Origins["/policy/"+rule].Source != "default" {
						t.Fatalf("lost cache policy default or origin: %s", rule)
					}
				}
			}
		})
	}
}
