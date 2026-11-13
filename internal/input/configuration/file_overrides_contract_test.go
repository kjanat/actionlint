package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestFileProjectionConsumesOverridesBeforeRuleBinding(t *testing.T) {
	t.Parallel()
	invalid, err := repoconfig.NewSetting("bogus", nil)
	if err != nil {
		t.Fatal(err)
	}
	project := &repoconfig.Config{Overrides: []repoconfig.Override{{Includes: []string{"bad.yml"}, Lint: &repoconfig.LintConfig{Rules: repoconfig.LintRulesConfig{Correctness: repoconfig.NewGroup("", "", map[string]repoconfig.Setting{"inline-suppression": invalid})}}}}}
	config, err := checking.ConfigurationRules().ForFile(project, "good.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Overrides) != 0 || len(project.Overrides) != 1 {
		t.Fatal("file projection did not consume overrides independently of the project")
	}
	if _, err := checking.ConfigurationRules().ResolveRules(repoconfig.Presets{}, config); err != nil {
		t.Fatalf("an unrelated override blocked this file: %v", err)
	}
	if _, err := checking.ConfigurationRules().ForFile(project, "bad.yml", ""); err == nil || !strings.Contains(err.Error(), "rule level") {
		t.Fatalf("the selected invalid override was accepted: %v", err)
	}
}

func TestFileOverrideRuleSettings(t *testing.T) {
	t.Parallel()
	cfg, err := checking.ConfigurationRules().Parse([]byte(`lint:
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
		got, err := checking.ConfigurationRules().ForFile(cfg, ".github/workflows/test.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		got = resolveRules(t, repoconfig.Presets{Strict: true}, got)
		if checking.ConfigurationRules().DiagnosticLevel(got, "if-cond") != "warn" || checking.ConfigurationRules().DiagnosticLevel(got, "expression-reference") != "off" {
			t.Fatal(got.Lint.Rules)
		}
		minimum, _ := got.RequiresJobTimeout().MinMinutes()
		maximum, _ := got.RequiresJobTimeout().MaxMinutes()
		if minimum != 5 || maximum != 60 || checking.ConfigurationRules().DiagnosticLevel(got, "require-job-timeout") != "warn" {
			t.Fatal(got.Policy)
		}
		reset, err := checking.ConfigurationRules().ForFile(cfg, ".github/workflows/reset.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if checking.ConfigurationRules().DiagnosticLevel(reset, "expression-reference") != "on" {
			t.Fatal(reset.Lint.Rules)
		}
		if checking.ConfigurationRules().DiagnosticLevel(cfg, "if-cond") != "off" {
			t.Fatal("override mutated base")
		}
		encoded, err := checking.ConfigurationRules().Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err = checking.ConfigurationRules().Parse(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileOverrides(t *testing.T) {
	t.Parallel()
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
	cfg, err := checking.ConfigurationRules().Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, roundtrip := range []bool{false, true} {
		if roundtrip {
			encoded, err := checking.ConfigurationRules().Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = checking.ConfigurationRules().Parse(encoded)
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
				got, err := checking.ConfigurationRules().ForFile(cfg, filepath.Join(root, filepath.FromSlash(tc.file)), root)
				if err != nil {
					t.Fatal(err)
				}
				if (len(got.Lint.Rules.Disable) != 0) != tc.expression || (checking.ConfigurationRules().DiagnosticLevel(got, "case-insensitive-conditions") != "off") != tc.cases || (checking.ConfigurationRules().DiagnosticLevel(got, "string-conditions") != "off") != tc.strings {
					t.Fatalf("%+v", got.Lint.Rules)
				}
			})
		}
	}
}

func TestInvalidFileOverrides(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"{}", "{includes: []}", "{includes: ['!**']}", "{includes: ['/tmp/*']}",
		"{includes: ['../*']}", "{includes: ['a/../b']}", "{includes: ['[']}",
		"{includes: [1]}", "{includes: ['**'], typo: true}", "{includes: ['**'], lint: {typo: true}}",
	} {
		if _, err := checking.ConfigurationRules().Parse([]byte("overrides: [" + value + "]")); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}

func TestProgrammaticLintOverridePreservesUnsetFields(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			for _, programmatic := range []bool{false, true} {
				if programmatic && tc.lint == nil {
					continue
				}
				cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {disable: [expression-reference]}}\noverrides: [{includes: ['**'], lint: " + tc.yaml + "}]"))
				if err != nil {
					t.Fatal(err)
				}
				if programmatic {
					cfg.Overrides = []repoconfig.Override{{Includes: []string{"**"}, Lint: tc.lint}}
				}
				for range 2 {
					effective, err := checking.ConfigurationRules().ForFile(cfg, "ci.yml", "")
					if err != nil || checking.ConfigurationRules().DiagnosticLevel(effective, "expression-reference") != tc.want {
						t.Fatalf("programmatic=%t: want %s, got %+v, %v", programmatic, tc.want, effective, err)
					}
					encoded, err := checking.ConfigurationRules().Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					cfg, err = checking.ConfigurationRules().Parse(encoded)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
