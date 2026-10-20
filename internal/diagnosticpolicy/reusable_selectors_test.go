package diagnosticpolicy_test

import (
	"path/filepath"
	"reflect"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
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
