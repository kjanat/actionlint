package engine

import (
	"path/filepath"
	"reflect"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestFileOverrideRuleSettings(t *testing.T) {
	cfg, err := ParseConfig([]byte(`lint:
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
		got = applyRulePresets(RulePresets{Strict: true}, got)
		if diagnosticLevel(got, "if-cond") != "warn" || diagnosticLevel(got, "expression-reference") != "off" {
			t.Fatal(got.Lint.Rules)
		}
		minimum, _ := got.RequiresJobTimeout().MinMinutes()
		maximum, _ := got.RequiresJobTimeout().MaxMinutes()
		if minimum != 5 || maximum != 60 || diagnosticLevel(got, "require-job-timeout") != "warn" {
			t.Fatal(got.Policy)
		}
		reset, err := repoconfig.ForFile(cfg, ".github/workflows/reset.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if diagnosticLevel(reset, "expression-reference") != "on" {
			t.Fatal(reset.Lint.Rules)
		}
		if diagnosticLevel(cfg, "if-cond") != "off" {
			t.Fatal("override mutated base")
		}
		encoded, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err = ParseConfig(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestProgrammaticLintOverridePreservesUnsetFields(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		lint       *LintConfig
		want       RuleLevel
	}{
		{"enabled", "{enabled: true}", &LintConfig{Enabled: new(true)}, "off"},
		{"empty", "{}", &LintConfig{}, "off"},
		{"clear", "{rules: {disable: []}}", &LintConfig{Rules: LintRulesConfig{Disable: []string{}}}, "on"},
		{"null-disable", "{rules: {disable: null}}", nil, "on"},
		{"null-rules", "{rules: null}", nil, "on"},
		{"null-lint", "null", nil, "on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, programmatic := range []bool{false, true} {
				if programmatic && tc.lint == nil {
					continue
				}
				cfg, err := ParseConfig([]byte("lint: {rules: {disable: [expression-reference]}}\noverrides: [{includes: ['**'], lint: " + tc.yaml + "}]"))
				if err != nil {
					t.Fatal(err)
				}
				if programmatic {
					cfg.Overrides = []ConfigOverride{{Includes: []string{"**"}, Lint: tc.lint}}
				}
				for range 2 {
					effective, err := repoconfig.ForFile(cfg, "ci.yml", "")
					if err != nil || diagnosticLevel(effective, "expression-reference") != tc.want {
						t.Fatalf("programmatic=%t: want %s, got %+v, %v", programmatic, tc.want, effective, err)
					}
					encoded, err := yaml.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					cfg, err = ParseConfig(encoded)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestRuleLevelAliasOverrides(t *testing.T) {
	for _, tc := range []struct {
		base, overlay string
		want          RuleLevel
	}{
		{"correctness: {expression-reference: error}", "correctness: {expression-reference: false}", "off"},
		{"correctness: {expression-reference: false}", "correctness: {expression-reference: null}", "on"},
		{"correctness: false", "correctness: {expression-reference: warn}", "warn"},
		{"correctness: false", "correctness: {expression-reference: {level: null}}", "off"},
		{"correctness: false", "correctness: null", "on"},
		{"correctness: {level: false}", "correctness: {level: null}", "on"},
		{"correctness: {expression-reference: off}", "correctness: {expression-reference: default}", "on"},
		{"correctness: off", "correctness: {expression-reference: {level: default}}", "off"},
		{"correctness: {expression-reference: off}", "correctness: default", "on"},
		{"correctness: {level: off}", "correctness: {level: default}", "on"},
	} {
		cfg, err := ParseConfig([]byte("lint: {rules: {" + tc.base + "}}\noverrides: [{includes: ['**'], lint: {rules: {" + tc.overlay + "}}}]"))
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			got, err := repoconfig.ForFile(cfg, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			if diagnosticLevel(got, "expression-reference") != tc.want {
				t.Fatalf("%s -> %s: got %s, want %s", tc.base, tc.overlay, diagnosticLevel(got, "expression-reference"), tc.want)
			}
			encoded, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = ParseConfig(encoded)
			if err != nil {
				t.Fatal(err)
			}
		}
		root := t.TempDir()
		writeShellcheckFixture(t, root, "base.yml", "lint: {rules: {"+tc.base+"}}")
		path := writeShellcheckFixture(t, root, "child.yml", "extends: [base.yml]\nlint: {rules: {"+tc.overlay+"}}")
		got, err := ReadConfigFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if diagnosticLevel(got, "expression-reference") != tc.want {
			t.Fatalf("extends %s -> %s: got %s, want %s", tc.base, tc.overlay, diagnosticLevel(got, "expression-reference"), tc.want)
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
	cfg, err := ParseConfig([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, roundtrip := range []bool{false, true} {
		if roundtrip {
			encoded, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = ParseConfig(encoded)
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
				if (len(got.Lint.Rules.Disable) != 0) != tc.expression || (diagnosticLevel(got, "case-insensitive-conditions") != "off") != tc.cases || (diagnosticLevel(got, "string-conditions") != "off") != tc.strings {
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
		if _, err := ParseConfig([]byte("overrides: [" + value + "]")); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}

func TestFileOverrideAnalysisIsolation(t *testing.T) {
	cfg, err := ParseConfig([]byte(`overrides:
  - includes: ['.github/workflows/suspicious.yml']
    lint: {rules: {suspicious: on}}
  - includes: ['.github/workflows/suppressed.yml']
    lint: {rules: {disable: [expression-reference, if-cond]}}
`))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	workflow := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    steps:
      - run: echo ok
`)
	bad := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    if: true
    steps:
      - run: echo '${{ typo.value }}'
`)
	request := AnalysisRequest{WorkingDir: root}
	for _, name := range []string{"normal", "suspicious", "suppressed"} {
		content := workflow
		if name == "suppressed" {
			content = bad
		}
		request.Sources = append(request.Sources, SourceUnit{Path: ".github/workflows/" + name + ".yml", Content: content, Config: cfg})
	}
	for range 3 {
		result, err := Analyze(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "case-insensitive-conditions" || result.Diagnostics[0].Path != ".github/workflows/suspicious.yml" {
			t.Fatal(result.Diagnostics)
		}
	}
	if !reflect.DeepEqual(cfg.Lint.Rules, LintRulesConfig{}) {
		t.Fatal("shared configuration mutated")
	}
}
