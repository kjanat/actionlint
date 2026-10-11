package actionlint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReusableIgnoreConfig(t *testing.T) {
	for _, source := range []string{
		`ignores: [{rule: expression, uses: './generated', file: '.github/workflows/*.yml', job: build, step: lint, reason: generated}]`,
		`{"ignores":[{"rule":"expression","uses":"./generated","file":".github/workflows/*.yml","job":"build","step":"lint","reason":"generated"}]}`,
	} {
		cfg, err := ParseConfig([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		want := IgnoreConfigs{{Rule: "expression", Uses: "./generated", File: ".github/workflows/*.yml", Job: "build", Step: "lint", Reason: "generated"}}
		if !reflect.DeepEqual(cfg.Ignores, want) {
			t.Fatalf("got %#v", cfg.Ignores)
		}
	}
	for _, entry := range []string{
		`{rule: expression}`, `{rule: unknown, uses: './generated'}`, `{rule: expression, file: '../outside'}`,
		`{rule: expression, file: '!foo'}`, `{rule: expression, file: '['}`, `{rule: expression, file: '/tmp/x'}`,
		`{rule: expression, step: ''}`, `{rule: expression, uses: './generated', reason: ''}`,
		`{rule: expression, uses: './generated', unknown: x}`, `{rule: expression, file: true}`,
		`{rule: expression, file: x, file: y}`, `{rule: expression, uses: null}`, `null`,
	} {
		t.Run(entry, func(t *testing.T) {
			if _, err := ParseConfig([]byte("ignores: [" + entry + "]")); err == nil {
				t.Fatalf("accepted invalid ignore %s", entry)
			}
		})
	}
}

func TestReusableIgnoreInheritance(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base.yaml")
	child := filepath.Join(root, "child.yaml")
	if err := os.WriteFile(base, []byte("ignores: [{rule: expression, job: build}]"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		overlay string
		want    int
		rule    string
	}{
		{"", 1, "expression"}, {"ignores: []", 0, ""}, {"ignores: null", 0, ""},
		{"ignores: [{rule: action, uses: './generated'}]", 1, "action"},
	} {
		if err := os.WriteFile(child, []byte("extends: [base.yaml]\n"+tc.overlay), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := ReadConfigFile(child)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Ignores) != tc.want || tc.want > 0 && cfg.Ignores[0].Rule != tc.rule {
			t.Fatalf("overlay %q: %#v", tc.overlay, cfg.Ignores)
		}
	}
}

const reusableIgnoreSource = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: generated
        name: Generated action
        uses: ./generated
        with:
          value: '${{ invalid }}'
      - uses: ./different
  other:
    runs-on: ubuntu-latest
    steps:
      - uses: ./generated
`

func TestReusableIgnoreSelectors(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		selector   string
		suppressed bool
	}{
		{`uses: './generated'`, true}, {`uses: './different'`, false},
		{`job: build`, true}, {`job: other`, false},
		{`step: generated`, true}, {`step: 'Generated action'`, true}, {`step: missing`, false},
		{`file: '.github/workflows/**'`, true}, {`file: '.github/workflows/no.yml'`, false},
		{`uses: './generated', job: build, step: generated, file: '.github/workflows/*.yml'`, true},
		{`uses: './generated', job: other`, false},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			cfg, err := ParseConfig([]byte("ignores: [{rule: expression, " + tc.selector + "}]"))
			if err != nil {
				t.Fatal(err)
			}
			finding := &Error{Kind: "expression", Line: 10, Column: 20}
			got := filterConfigIgnores(filepath.Join(root, ".github", "workflows", "ci.yml"), root, []byte(reusableIgnoreSource), []*Error{finding}, cfg, false, cfg.diagnosticLevel)
			if (len(got) == 0) != tc.suppressed {
				t.Fatalf("got %#v", got)
			}
		})
	}
}

func TestReusableIgnorePolicyAndForeignSources(t *testing.T) {
	for _, tc := range []struct {
		policy string
		kinds  []string
	}{
		{"false", nil}, {"{report: violation}", []string{"expression"}},
		{"{report: suppression}", []string{"disallow-suppressions"}},
		{"true", []string{"disallow-suppressions", "expression"}},
	} {
		cfg, err := ParseConfig([]byte("ignores: [{rule: expression, uses: './generated'}]\npolicy: {disallow-suppressions: " + tc.policy + "}"))
		if err != nil {
			t.Fatal(err)
		}
		finding := &Error{Kind: "expression", Line: 10, Column: 20}
		got := filterConfigIgnores("ci.yml", "", []byte(reusableIgnoreSource), []*Error{finding}, cfg, false, cfg.diagnosticLevel)
		var kinds []string
		for _, e := range got {
			kinds = append(kinds, e.Kind)
		}
		if !reflect.DeepEqual(kinds, tc.kinds) {
			t.Fatalf("policy %s: %#v", tc.policy, got)
		}
	}
	cfg, err := ParseConfig([]byte("ignores: [{rule: expression, uses: './generated'}]"))
	if err != nil {
		t.Fatal(err)
	}
	foreign := &Error{Kind: "expression", Line: 10, Column: 20, Filepath: "action.yml", source: []byte("foreign")}
	if got := filterConfigIgnores("ci.yml", "", []byte(reusableIgnoreSource), []*Error{foreign}, cfg, false, cfg.diagnosticLevel); len(got) != 1 {
		t.Fatalf("caller suppressed foreign finding: %#v", got)
	}
	foreign.suppressionOrigin = &suppressionOrigin{path: "other.yml", first: 10, last: 10}
	if got := filterConfigIgnores("ci.yml", "", []byte(reusableIgnoreSource), []*Error{foreign}, cfg, false, cfg.diagnosticLevel); len(got) != 1 {
		t.Fatalf("caller suppressed foreign origin: %#v", got)
	}
}

func TestReusableIgnoreAnalysisIntegration(t *testing.T) {
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: check
        run: echo '${{ invalid }}'
`
	if findings := lintCachePolicy(t, source, "ignores: [{rule: expression, job: build, step: check}]"); len(findings) != 0 {
		t.Fatal(findings)
	}
	if findings := lintCachePolicy(t, source, "ignores: [{rule: expression, job: other}]"); len(findings) != 1 || findings[0].Kind != "expression" {
		t.Fatal(findings)
	}
}

func TestReusableIgnoreLiteralSelectors(t *testing.T) {
	for _, field := range []struct{ key, value, selector string }{
		{"uses", "./generated", "uses"},
		{"id", "generated", "step"},
		{"name", "Generated action", "step"},
	} {
		t.Run(field.key, func(t *testing.T) {
			source := strings.Replace(reusableIgnoreSource, field.key+": "+field.value, field.key+": ${{ '"+field.value+"' }}", 1)
			config := "ignores: [{rule: expression, " + field.selector + ": '" + field.value + "'}]"
			if findings := lintCachePolicy(t, source, config); len(findings) != 0 {
				t.Fatalf("literal selector did not suppress finding: %+v", findings)
			}
		})
	}
}

func TestReusableIgnoreDisabledRulePolicy(t *testing.T) {
	for _, report := range []string{"suppression", "all", "violation"} {
		t.Run(report, func(t *testing.T) {
			config := "lint: {rules: {correctness: {expression: off}}}\nignores: [{rule: expression, job: build}]\npolicy: {disallow-suppressions: {report: " + report + "}}"
			if findings := lintCachePolicy(t, reusableIgnoreSource, config); len(findings) != 0 {
				t.Fatalf("disabled finding triggered suppression policy: %+v", findings)
			}
		})
	}
}

func TestReusableIgnoreDisabledCompositeRule(t *testing.T) {
	for _, level := range []string{"off", "error"} {
		t.Run(level, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "lint: {rules: {correctness: {action: "+level+"}}}\nignores: [{rule: action, file: inner/action.yml, step: validate}]\npolicy: {disallow-suppressions: {report: suppression}}\n")
			writeShellcheckFixture(t, root, "inner/action.yml", "name: Inner\ndescription: Inner\nruns:\n  using: composite\n  steps:\n    - id: validate\n      shell: bash\n      run: echo '${{ invalid }}'\n")
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			if level == "off" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("disabled composite finding triggered policy: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "disallow-suppressions" {
				t.Fatalf("enabled composite finding bypassed policy: %+v", result.Diagnostics)
			}
		})
	}
}

func TestReusableIgnoreLiteralMissingAction(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "ignores: [{rule: missing-local-action, uses: ./generated}]\n")
	result := compositeAnalysis(t, root, "- uses: ${{ './generated' }}\n  id: generated", AnalysisOptions{})
	if len(result.Diagnostics) != 0 {
		t.Fatalf("literal missing action was not suppressed: %+v", result.Diagnostics)
	}
}

func TestReusableIgnoreMetadataScope(t *testing.T) {
	for _, tc := range []struct {
		selector string
		want     int
	}{
		{`uses: './inner'`, 1},
		{`file: 'inner/action.yml', step: validate`, 0},
		{`file: '.github/workflows/**', step: validate`, 1},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\nignores: [{rule: action, "+tc.selector+"}]")
			writeShellcheckFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - id: validate
      shell: bash
      run: echo '${{ invalid }}'
`)
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("got %+v", result.Diagnostics)
			}
		})
	}
}

func TestReusableIgnoreCannotSuppressPolicy(t *testing.T) {
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ invalid }}' # actionlint:ignore expression
`
	findings := lintCachePolicy(t, source, `ignores: [{rule: disallow-suppressions, file: '*.yaml'}]
policy: {disallow-suppressions: {rules: [expression], report: suppression}}`)
	if len(findings) != 1 || findings[0].Kind != "disallow-suppressions" {
		t.Fatal(findings)
	}
}
