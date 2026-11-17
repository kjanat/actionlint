package configuration

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
			config, err := rules.ParseConfig([]byte("tools: {shellcheck: " + tc.base + "}\noverrides: [{includes: [ci.yml], tools: {shellcheck: " + tc.override + "}}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := rules.ForFile(config, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			config.Overrides[0] = Override{Includes: []string{"ci.yml"}, Tools: config.Overrides[0].Tools}
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
				config, err = rules.ParseConfig(encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestParsedOverrideRetainsCapturedPrecedence(t *testing.T) {
	t.Parallel()
	rules := RuleSet{}
	cfg, err := rules.ParseConfig([]byte("overrides: [{includes: ['**'], lint: {enabled: false}}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Overrides[0].Lint.Enabled = new(true)
	got, err := rules.ForFile(cfg, "ci.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Lint.Enabled == nil || *got.Lint.Enabled {
		t.Fatalf("typed mutation replaced the captured partial: %+v", got.Lint.Enabled)
	}
}
