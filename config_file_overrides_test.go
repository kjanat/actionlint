package actionlint

import (
	"path/filepath"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v4"
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
		got, err := configForFile(cfg, ".github/workflows/test.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		got = (RulePresets{Strict: true}).apply(got)
		if got.diagnosticLevel("if-cond") != "warn" || got.diagnosticLevel("expression") != "off" {
			t.Fatal(got.Lint.Rules)
		}
		minimum, _ := got.RequiresJobTimeout().MinMinutes()
		maximum, _ := got.RequiresJobTimeout().MaxMinutes()
		if minimum != 5 || maximum != 60 || got.diagnosticLevel("require-job-timeout") != "warn" {
			t.Fatal(got.Policy)
		}
		reset, err := configForFile(cfg, ".github/workflows/reset.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		if reset.diagnosticLevel("expression") != "on" {
			t.Fatal(reset.Lint.Rules)
		}
		if cfg.diagnosticLevel("if-cond") != "off" {
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

func TestRuleLevelAliasOverrides(t *testing.T) {
	for _, tc := range []struct {
		base, overlay string
		want          RuleLevel
	}{
		{"correctness: {expression: error}", "correctness: {expression: false}", "off"},
		{"correctness: {expression: false}", "correctness: {expression: null}", "on"},
		{"correctness: false", "correctness: {expression: warn}", "warn"},
		{"correctness: false", "correctness: {expression: {level: null}}", "off"},
		{"correctness: false", "correctness: null", "on"},
		{"correctness: {level: false}", "correctness: {level: null}", "on"},
		{"correctness: {expression: off}", "correctness: {expression: default}", "on"},
		{"correctness: off", "correctness: {expression: {level: default}}", "off"},
		{"correctness: {expression: off}", "correctness: default", "on"},
		{"correctness: {level: off}", "correctness: {level: default}", "on"},
	} {
		cfg, err := ParseConfig([]byte("lint: {rules: {" + tc.base + "}}\noverrides: [{includes: ['**'], lint: {rules: {" + tc.overlay + "}}}]"))
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			got, err := configForFile(cfg, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			if got.diagnosticLevel("expression") != tc.want {
				t.Fatalf("%s -> %s: got %s, want %s", tc.base, tc.overlay, got.diagnosticLevel("expression"), tc.want)
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
		if got.diagnosticLevel("expression") != tc.want {
			t.Fatalf("extends %s -> %s: got %s, want %s", tc.base, tc.overlay, got.diagnosticLevel("expression"), tc.want)
		}
	}
}

func TestFileOverrides(t *testing.T) {
	const source = `lint:
  rules:
    disable: [expression]
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
				got, err := configForFile(cfg, filepath.Join(root, filepath.FromSlash(tc.file)), root)
				if err != nil {
					t.Fatal(err)
				}
				if (len(got.Lint.Rules.Disable) != 0) != tc.expression || (got.diagnosticLevel("case-insensitive-conditions") != "off") != tc.cases || (got.diagnosticLevel("string-conditions") != "off") != tc.strings {
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
    lint: {rules: {disable: [expression, if-cond]}}
`))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	workflow := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    if: github.ref == 'refs/heads/main'\n    steps:\n      - run: echo ok\n")
	bad := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    if: true\n    steps:\n      - run: echo '${{ typo.value }}'\n")
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
