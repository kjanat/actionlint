package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/workspace"
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
			cfg, err := repoconfig.Parse([]byte("ignores: [{rule: expression-reference, " + tc.selector + "}]"))
			if err != nil {
				t.Fatal(err)
			}
			finding := &findingmodel.Finding{Rule: "expression-reference", Start: diagmodel.Position{Line: 10, Column: 20}}
			got := diagnosticpolicy.FilterConfigured(filepath.Join(root, ".github", "workflows", "ci.yml"), root, []byte(reusableIgnoreSource), []*findingmodel.Finding{finding}, cfg, false, diagnosticpolicy.Levels(cfg))
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
		{"false", nil}, {"{report: violation}", []string{"expression-reference"}},
		{"{report: suppression}", []string{"disallow-suppressions"}},
		{"true", []string{"disallow-suppressions", "expression-reference"}},
	} {
		cfg, err := repoconfig.Parse([]byte("ignores: [{rule: expression-reference, uses: './generated'}]\npolicy: {disallow-suppressions: " + tc.policy + "}"))
		if err != nil {
			t.Fatal(err)
		}
		finding := &findingmodel.Finding{Rule: "expression-reference", Start: diagmodel.Position{Line: 10, Column: 20}}
		got := diagnosticpolicy.FilterConfigured("ci.yml", "", []byte(reusableIgnoreSource), []*findingmodel.Finding{finding}, cfg, false, diagnosticpolicy.Levels(cfg))
		var kinds []string
		for _, e := range got {
			kinds = append(kinds, e.Rule)
		}
		if !reflect.DeepEqual(kinds, tc.kinds) {
			t.Fatalf("policy %s: %#v", tc.policy, got)
		}
	}
	cfg, err := repoconfig.Parse([]byte("ignores: [{rule: expression-reference, uses: './generated'}]"))
	if err != nil {
		t.Fatal(err)
	}
	foreign := &findingmodel.Finding{Rule: "expression-reference", Start: diagmodel.Position{Line: 10, Column: 20}, Path: "action.yml", Source: []byte("foreign")}
	if got := diagnosticpolicy.FilterConfigured("ci.yml", "", []byte(reusableIgnoreSource), []*findingmodel.Finding{foreign}, cfg, false, diagnosticpolicy.Levels(cfg)); len(got) != 1 {
		t.Fatalf("caller suppressed foreign finding: %#v", got)
	}
	foreign.SuppressionOrigin = &findingmodel.SuppressionOrigin{Path: "other.yml", First: 10, Last: 10}
	if got := diagnosticpolicy.FilterConfigured("ci.yml", "", []byte(reusableIgnoreSource), []*findingmodel.Finding{foreign}, cfg, false, diagnosticpolicy.Levels(cfg)); len(got) != 1 {
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
	if findings := lintCachePolicy(t, source, "ignores: [{rule: expression-reference, job: build, step: check}]"); len(findings) != 0 {
		t.Fatal(findings)
	}
	if findings := lintCachePolicy(t, source, "ignores: [{rule: expression-reference, job: other}]"); len(findings) != 1 || findings[0].Rule != "expression-reference" {
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
			if findings := lintCachePolicy(t, source, config); len(findings) != 0 {
				t.Fatalf("literal selector did not suppress finding: %+v", findings)
			}
		})
	}
}

func TestReusableIgnoreDisabledRulePolicy(t *testing.T) {
	for _, report := range []string{"suppression", "all", "violation"} {
		t.Run(report, func(t *testing.T) {
			config := "lint: {rules: {correctness: {expression-reference: off}}}\nignores: [{rule: expression-reference, job: build}]\npolicy: {disallow-suppressions: {report: " + report + "}}"
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

func TestReusableIgnoreCallerRelativeFile(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	root := filepath.Join(parent, "repo")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ invalid }}'\n"
	workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", source)
	for _, pattern := range []string{".github/workflows/**", ".github/workflows/other.yml"} {
		t.Run(pattern, func(t *testing.T) {
			config := "ignores: [{rule: expression-reference, file: '" + pattern + "'}]"
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
			cfg, err := repoconfig.Parse([]byte(config))
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if strings.HasSuffix(pattern, "other.yml") {
				want = 1
			}
			for _, path := range []string{workflow, filepath.Join("repo", ".github", "workflows", "ci.yml")} {
				session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: parent})
				if err != nil {
					t.Fatal(err)
				}
				result, err := session.Files([]string{path}, nil)
				if err != nil || result.FileCount != 1 || len(result.Diagnostics) != want {
					t.Fatalf("session %s: %+v, %v", path, result, err)
				}
				result, err = Analyze(t.Context(), AnalysisRequest{WorkingDir: parent, Sources: []SourceUnit{{Path: path, Content: []byte(source), Project: workspace.Identity(root), Config: cfg}}})
				if err != nil || result.FileCount != 1 || len(result.Diagnostics) != want {
					t.Fatalf("direct %s: %+v, %v", path, result, err)
				}
			}
		})
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
	_, err := repoconfig.Parse([]byte(`ignores: [{rule: disallow-suppressions, file: '*.yaml'}]`))
	if err == nil || !strings.Contains(err.Error(), "disallow-suppressions") {
		t.Fatalf("ineffective suppression was accepted: %v", err)
	}
}

func TestReusableIgnoreReportedFile(t *testing.T) {
	root := t.TempDir()
	workflow := filepath.Join(root, ".github", "workflows", "ci.yml")
	script := filepath.Join(root, "scripts", "check.sh")
	for _, tc := range []struct {
		selector string
		want     int
	}{
		{"file: scripts/check.sh", 0},
		{"file: .github/workflows/ci.yml", 1},
		{"file: scripts/check.sh, job: build, step: generated", 0},
		{"file: scripts/check.sh, job: other", 1},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte("ignores: [{rule: shellcheck, " + tc.selector + "}]"))
			if err != nil {
				t.Fatal(err)
			}
			finding := &findingmodel.Finding{Rule: "shellcheck", Start: diagmodel.Position{Line: 1, Column: 6}, Path: script, Source: []byte("echo $VALUE\n"), SuppressionOrigin: &findingmodel.SuppressionOrigin{First: 10, Last: 10}}
			got := diagnosticpolicy.FilterConfigured(workflow, root, []byte(reusableIgnoreSource), []*findingmodel.Finding{finding}, cfg, false, diagnosticpolicy.Levels(cfg))
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d", len(got), tc.want)
			}
		})
	}
}
