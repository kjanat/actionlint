package config_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/internal/yaml"
)

func TestRuleLevelAliasOverrides(t *testing.T) {
	for _, tc := range []struct {
		base, overlay string
		want          repoconfig.Level
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
		cfg, err := repoconfig.Parse([]byte("lint: {rules: {" + tc.base + "}}\noverrides: [{includes: ['**'], lint: {rules: {" + tc.overlay + "}}}]"))
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			got, err := repoconfig.ForFile(cfg, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			if repoconfig.DiagnosticLevel(got, "expression-reference") != tc.want {
				t.Fatalf("%s -> %s: got %s, want %s", tc.base, tc.overlay, repoconfig.DiagnosticLevel(got, "expression-reference"), tc.want)
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
		root := t.TempDir()
		testutil.WriteFixture(t, root, "base.yml", "lint: {rules: {"+tc.base+"}}")
		path := testutil.WriteFixture(t, root, "child.yml", "extends: [base.yml]\nlint: {rules: {"+tc.overlay+"}}")
		got, err := repoconfig.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if repoconfig.DiagnosticLevel(got, "expression-reference") != tc.want {
			t.Fatalf("extends %s -> %s: got %s, want %s", tc.base, tc.overlay, repoconfig.DiagnosticLevel(got, "expression-reference"), tc.want)
		}
	}
}

func TestFileOverrideAnalysisIsolation(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`overrides:
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
	request := application.Options{WorkingDir: root}
	var requestSources []workspace.Source
	for _, name := range []string{"normal", "suspicious", "suppressed"} {
		content := workflow
		if name == "suppressed" {
			content = bad
		}
		requestSources = append(requestSources, workspace.Source{Path: ".github/workflows/" + name + ".yml", Content: content, Config: cfg})
	}
	for range 3 {
		result, err := application.Analyze(t.Context(), request, requestSources)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "case-insensitive-conditions" || result.Diagnostics[0].Path != ".github/workflows/suspicious.yml" {
			t.Fatal(result.Diagnostics)
		}
	}
	if !reflect.DeepEqual(cfg.Lint.Rules, repoconfig.LintRulesConfig{}) {
		t.Fatal("shared configuration mutated")
	}
}
