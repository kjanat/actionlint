package suppression_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

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

func TestReusableIgnoreAnalysisIntegration(t *testing.T) {
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: check
        run: echo '${{ invalid }}'
`
	if findings := testutil.WorkflowDiagnostics(t, source, "ignores: [{rule: expression-reference, job: build, step: check}]"); len(findings) != 0 {
		t.Fatal(findings)
	}
	if findings := testutil.WorkflowDiagnostics(t, source, "ignores: [{rule: expression-reference, job: other}]"); len(findings) != 1 || findings[0].Rule != "expression-reference" {
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
			config := "ignores: [{rule: expression-reference, " + field.selector + ": '" + field.value + "'}]"
			if findings := testutil.WorkflowDiagnostics(t, source, config); len(findings) != 0 {
				t.Fatalf("literal selector did not suppress finding: %+v", findings)
			}
		})
	}
}

func TestReusableIgnoreDisabledRulePolicy(t *testing.T) {
	for _, report := range []string{"suppression", "all", "violation"} {
		t.Run(report, func(t *testing.T) {
			config := "lint: {rules: {correctness: {expression-reference: off}}}\nignores: [{rule: expression-reference, job: build}]\npolicy: {disallow-suppressions: {report: " + report + "}}"
			if findings := testutil.WorkflowDiagnostics(t, reusableIgnoreSource, config); len(findings) != 0 {
				t.Fatalf("disabled finding triggered suppression policy: %+v", findings)
			}
		})
	}
}

func TestReusableIgnoreDisabledCompositeRule(t *testing.T) {
	for _, level := range []string{"off", "error"} {
		t.Run(level, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "lint: {rules: {correctness: {action: "+level+"}}}\nignores: [{rule: action, file: inner/action.yml, step: validate}]\npolicy: {disallow-suppressions: {report: suppression}}\n")
			testutil.WriteFixture(t, root, "inner/action.yml", "name: Inner\ndescription: Inner\nruns:\n  using: composite\n  steps:\n    - id: validate\n      shell: bash\n      run: echo '${{ invalid }}'\n")
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
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
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "ignores: [{rule: missing-local-action, uses: ./generated}]\n")
	result := testutil.Composite(t, root, "- uses: ${{ './generated' }}\n  id: generated", application.Options{})
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
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\nignores: [{rule: action, "+tc.selector+"}]")
			testutil.WriteFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - id: validate
      shell: bash
      run: echo '${{ invalid }}'
`)
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("got %+v", result.Diagnostics)
			}
		})
	}
}
