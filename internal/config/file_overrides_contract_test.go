package config_test

import (
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/yaml"
)

func TestFileOverrideRuleSettings(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`lint:
  rules:
    correctness: off
    policy:
      require-job-timeout: {level: error, options: {min-minutes: 5, max-minutes: 30}}
overrides:
  - includes: ['**/test.yml']
    lint:
      rules:
        correctness: {if-cond: warn}
        policy:
          require-job-timeout: {level: warn, options: {max-minutes: 60}}
  - includes: ['**/reset.yml']
    lint: {rules: {correctness: null}}
`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := repoconfig.ForFile(cfg, ".github/workflows/test.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		got = repoconfig.ResolveRules(repoconfig.Presets{Strict: true}, got)
		if repoconfig.DiagnosticLevel(got, "if-cond") != "warn" || repoconfig.DiagnosticLevel(got, "expression-reference") != "off" {
			t.Fatal(got.Lint.Rules)
		}
		minimum, _ := got.RequiresJobTimeout().MinMinutes()
		maximum, _ := got.RequiresJobTimeout().MaxMinutes()
		if minimum != 5 || maximum != 60 || repoconfig.DiagnosticLevel(got, "require-job-timeout") != "warn" {
			t.Fatal(got.Policy)
		}
		reset, err := repoconfig.ForFile(cfg, ".github/workflows/reset.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if repoconfig.DiagnosticLevel(reset, "expression-reference") != "on" {
			t.Fatal(reset.Lint.Rules)
		}
		if repoconfig.DiagnosticLevel(cfg, "if-cond") != "off" {
			t.Fatal("override mutated base")
		}
		encoded, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err = repoconfig.Parse(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileOverrides(t *testing.T) {
	const source = `lint:
  rules:
    disable: [expression-reference]
    suspicious: on
overrides:
  - includes: ['.github/workflows/**', '!.github/workflows/release.yml']
    lint:
      rules:
        suspicious: {case-insensitive-conditions: off}
  - includes: ['.github/workflows/test.yml']
    lint: {rules: {disable: [], suspicious: off}}
  - includes: ['.github/workflows/reset.yml']
    lint: null
  - includes: ['.github/workflows/noop.yml']
`
	cfg, err := repoconfig.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, roundtrip := range []bool{false, true} {
		if roundtrip {
			encoded, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = repoconfig.Parse(encoded)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, tc := range []struct {
			file                       string
			expression, cases, strings bool
		}{
			{"elsewhere.yml", true, true, true},
			{".github/workflows/release.yml", true, true, true},
			{".github/workflows/ci.yml", true, false, true},
			{".github/workflows/nested/ci.yml", true, false, true},
			{".github/workflows/test.yml", false, false, false},
			{".github/workflows/reset.yml", false, false, false},
			{".github/workflows/noop.yml", true, false, true},
		} {
			cfg := cfg
			t.Run(tc.file, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				got, err := repoconfig.ForFile(cfg, filepath.Join(root, filepath.FromSlash(tc.file)), root)
				if err != nil {
					t.Fatal(err)
				}
				if (len(got.Lint.Rules.Disable) != 0) != tc.expression || (repoconfig.DiagnosticLevel(got, "case-insensitive-conditions") != "off") != tc.cases || (repoconfig.DiagnosticLevel(got, "string-conditions") != "off") != tc.strings {
					t.Fatalf("%+v", got.Lint.Rules)
				}
			})
		}
	}
}

func TestInvalidFileOverrides(t *testing.T) {
	for _, value := range []string{
		"{}", "{includes: []}", "{includes: ['!**']}", "{includes: ['/tmp/*']}",
		"{includes: ['../*']}", "{includes: ['a/../b']}", "{includes: ['[']}",
		"{includes: [1]}", "{includes: ['**'], typo: true}", "{includes: ['**'], lint: {typo: true}}",
	} {
		if _, err := repoconfig.Parse([]byte("overrides: [" + value + "]")); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}

func TestProgrammaticLintOverridePreservesUnsetFields(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		lint       *repoconfig.LintConfig
		want       repoconfig.Level
	}{
		{"enabled", "{enabled: true}", &repoconfig.LintConfig{Enabled: new(true)}, "off"},
		{"empty", "{}", &repoconfig.LintConfig{}, "off"},
		{"clear", "{rules: {disable: []}}", &repoconfig.LintConfig{Rules: repoconfig.LintRulesConfig{Disable: []string{}}}, "on"},
		{"null-disable", "{rules: {disable: null}}", nil, "on"},
		{"null-rules", "{rules: null}", nil, "on"},
		{"null-lint", "null", nil, "on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, programmatic := range []bool{false, true} {
				if programmatic && tc.lint == nil {
					continue
				}
				cfg, err := repoconfig.Parse([]byte("lint: {rules: {disable: [expression-reference]}}\noverrides: [{includes: ['**'], lint: " + tc.yaml + "}]"))
				if err != nil {
					t.Fatal(err)
				}
				if programmatic {
					cfg.Overrides = []repoconfig.Override{{Includes: []string{"**"}, Lint: tc.lint}}
				}
				for range 2 {
					effective, err := repoconfig.ForFile(cfg, "ci.yml", "")
					if err != nil || repoconfig.DiagnosticLevel(effective, "expression-reference") != tc.want {
						t.Fatalf("programmatic=%t: want %s, got %+v, %v", programmatic, tc.want, effective, err)
					}
					encoded, err := yaml.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					cfg, err = repoconfig.Parse(encoded)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
