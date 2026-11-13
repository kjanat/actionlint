package config

import (
	"reflect"
	"testing"
)

func TestProgrammaticToolOverridesPreserveOmissions(t *testing.T) {
	t.Parallel()
	rules := RuleSet{}
	for _, tc := range []struct{ name, base, override string }{
		{"disabled", "{enabled: true, config: .shellcheckrc}", "{enabled: false}"},
		{"enabled", "{enabled: false, config: {disable: [SC2086]}}", "{enabled: true}"},
		{"config only", "{enabled: false, config: .shellcheckrc}", "{config: replacement.rc}"},
		{"empty", "{enabled: false, config: .shellcheckrc}", "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config, err := rules.Parse([]byte("tools: {shellcheck: " + tc.base + "}\noverrides: [{includes: [ci.yml], tools: {shellcheck: " + tc.override + "}}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := rules.ForFile(config, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			config.Overrides[0].toolsNode = nil
			config.Overrides[0].toolsSet = false
			for range 2 {
				got, err := rules.ForFile(config, "ci.yml", "")
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got.Tools, want.Tools) {
					t.Fatalf("programmatic tools differ: got %#v, want %#v", got.Tools["shellcheck"], want.Tools["shellcheck"])
				}
				encoded, err := rules.Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				config, err = rules.Parse(encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
