package policy_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
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
	t.Parallel()
	for _, tc := range []struct {
		policy string
		kinds  []string
	}{
		{"false", nil},
		{"{report: violation}", []string{"expression-reference"}},
		{"{report: suppression}", []string{"forbidden-suppression"}},
		{"true", []string{"forbidden-suppression", "expression-reference"}},
	} {
		cfg, err := checking.ConfigurationRules().ParseConfig([]byte("ignores: [{rule: expression-reference, uses: './generated'}]\npolicy: {disallow-suppressions: " + tc.policy + "}"))
		if err != nil {
			t.Fatal(err)
		}
		item := &finding.Finding{Rule: "expression-reference", Start: sourcetext.Position{Line: 10, Column: 20}}
		got := fixtureConfigured(t, "ci.yml", "", []byte(reusableIgnoreSource), []*finding.Finding{item}, cfg)
		var kinds []string
		for _, e := range got {
			kinds = append(kinds, e.Rule)
		}
		slices.Sort(kinds)
		slices.Sort(tc.kinds)
		if !reflect.DeepEqual(kinds, tc.kinds) {
			t.Fatalf("policy %s: %#v", tc.policy, got)
		}
	}
	cfg, err := checking.ConfigurationRules().ParseConfig([]byte("ignores: [{rule: expression-reference, uses: './generated'}]"))
	if err != nil {
		t.Fatal(err)
	}
	foreign := &finding.Finding{Rule: "expression-reference", Start: sourcetext.Position{Line: 10, Column: 20}, Path: "action.yml", Source: results.DiagnosticSource{Content: []byte("foreign")}}
	if got := fixtureConfigured(t, "ci.yml", "", []byte(reusableIgnoreSource), []*finding.Finding{foreign}, cfg); len(got) != 1 {
		t.Fatalf("caller suppressed foreign finding: %#v", got)
	}
	foreign.SuppressionOrigin = &results.SuppressionOrigin{Path: "other.yml", Lines: sourcetext.LineRange{First: 10, Last: 10}}
	if got := fixtureConfigured(t, "ci.yml", "", []byte(reusableIgnoreSource), []*finding.Finding{foreign}, cfg); len(got) != 1 {
		t.Fatalf("caller suppressed foreign origin: %#v", got)
	}
}

func TestReusableIgnoreReportedFile(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			cfg, err := checking.ConfigurationRules().ParseConfig([]byte("ignores: [{rule: shellcheck, " + tc.selector + "}]"))
			if err != nil {
				t.Fatal(err)
			}
			item := &finding.Finding{Rule: "shellcheck", Start: sourcetext.Position{Line: 1, Column: 6}, Path: script, Source: results.DiagnosticSource{Content: []byte("echo $VALUE\n")}, SuppressionOrigin: &results.SuppressionOrigin{Lines: sourcetext.LineRange{First: 10, Last: 10}}}
			got := fixtureConfigured(t, workflow, root, []byte(reusableIgnoreSource), []*finding.Finding{item}, cfg)
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d", len(got), tc.want)
			}
		})
	}
}

func TestReusableIgnoreSelectors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, tc := range []struct {
		selector   string
		suppressed bool
	}{
		{`uses: './generated'`, true},
		{`uses: './different'`, false},
		{`job: build`, true},
		{`job: other`, false},
		{`step: generated`, true},
		{`step: 'Generated action'`, true},
		{`step: missing`, false},
		{`file: '.github/workflows/**'`, true},
		{`file: '.github/workflows/no.yml'`, false},
		{`uses: './generated', job: build, step: generated, file: '.github/workflows/*.yml'`, true},
		{`uses: './generated', job: other`, false},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().ParseConfig([]byte("ignores: [{rule: expression-reference, " + tc.selector + "}]"))
			if err != nil {
				t.Fatal(err)
			}
			item := &finding.Finding{Rule: "expression-reference", Start: sourcetext.Position{Line: 10, Column: 20}}
			got := fixtureConfigured(t, filepath.Join(root, ".github", "workflows", "ci.yml"), root, []byte(reusableIgnoreSource), []*finding.Finding{item}, cfg)
			if (len(got) == 0) != tc.suppressed {
				t.Fatalf("got %#v", got)
			}
		})
	}
}
