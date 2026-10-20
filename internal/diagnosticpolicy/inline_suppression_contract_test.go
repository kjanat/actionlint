package diagnosticpolicy_test

import (
	"fmt"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"

	"github.com/google/go-cmp/cmp"
)

func TestInlineSuppressionBlockBody(t *testing.T) {
	for _, header := range []string{
		"run: | # actionlint:ignore expression-reference,shellcheck -- reviewed",
		"run: >- # actionlint:ignore expression-reference,shellcheck -- reviewed",
		"run: |2+ # actionlint:ignore expression-reference,shellcheck -- reviewed",
		"# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed\nrun: |",
		"run: &script | # actionlint:ignore expression-reference,shellcheck -- reviewed",
		"run: !!str\n  | # actionlint:ignore expression-reference,shellcheck -- reviewed",
		`# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed
run: !!str
  |`,
		`# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed
run: &script
  |2-`,
		`# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed
run: &script !!str
  >-`,
		`run: # actionlint:ignore expression-reference,shellcheck -- reviewed
  !!str
  |`,
		`run:
  !!str
  &script # actionlint:ignore expression-reference,shellcheck -- reviewed
  |`,
		`# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed
run:
  &script
  !!str
  >-`,
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(header+ending, func(t *testing.T) {
				start := strings.Count(header, "\n") + 1
				source := strings.ReplaceAll(header+`
    echo first
    echo second
next: value
`, "\n", ending)
				findings := []*findingmodel.Finding{
					{Start: diagmodel.Position{Line: start + 1}, Rule: "expression-reference"},
					{Start: diagmodel.Position{Line: start + 2}, Rule: "shellcheck"},
					{Start: diagmodel.Position{Line: start + 2}, Rule: "action"},
					{Start: diagmodel.Position{Line: start + 3}, Rule: "expression-reference"},
				}
				got := diagnosticpolicy.FilterInline([]byte(source), findings, nil)
				if diff := cmp.Diff(findings[2:], got, cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestInlineSuppressionRegistry(t *testing.T) {
	for _, name := range diagnosticpolicy.SuppressionRules() {
		t.Run(name, func(t *testing.T) {
			cfg, err := repoconfig.Parse(fmt.Appendf(nil, "policy: {disallow-suppressions: {rules: [%s]}}", name))
			if err != nil {
				t.Fatal(err)
			}
			source := []byte("first: value\nsecond: value\nthird: value\n# actionlint:ignore-next-line " + name + " -- reviewed\nfifth: value\n")
			finding := &findingmodel.Finding{Start: diagmodel.Position{Line: 5}, Rule: name}
			if got := diagnosticpolicy.FilterInline(source, []*findingmodel.Finding{finding}, nil); len(got) != 0 {
				t.Fatalf("registered rule was not suppressed: %v", got)
			}
			got := diagnosticpolicy.FilterInline(source, []*findingmodel.Finding{finding}, cfg.Policy.DisallowSuppressions)
			if len(got) != 2 || got[0] != finding || got[1].Rule != "disallow-suppressions" || got[1].Start.Line != 4 {
				t.Fatalf("restriction did not retain finding and report comment: %v", got)
			}
		})
	}
}

func TestInlineSuppressionScriptTextIsNotDirective(t *testing.T) {
	source := []byte(`run: |
  text = '''
  # actionlint:ignore-next-line expression-reference -- sample text
  ${{ github.event.issue.title }}
  '''
`)
	finding := &findingmodel.Finding{Start: diagmodel.Position{Line: 4}, Rule: "expression-reference"}
	got := diagnosticpolicy.FilterInline(source, []*findingmodel.Finding{finding}, nil)
	if len(got) != 1 || got[0] != finding {
		t.Fatalf("script text changed findings: %+v", got)
	}
}

func TestInlineSuppressionSplitPrefixIsolation(t *testing.T) {
	for _, header := range []string{
		`env: {SAFE: value} # actionlint:ignore expression-reference,shellcheck -- unrelated
run:
  !!str
  |`,
		`run:
  !!str
  |
    # actionlint:ignore expression-reference,shellcheck -- script text`,
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(header+"\n    echo body\n", "\n", ending)
			line := strings.Count(header, "\n") + 2
			findings := []*findingmodel.Finding{{Start: diagmodel.Position{Line: line}, Rule: "expression-reference"}, {Start: diagmodel.Position{Line: line}, Rule: "shellcheck"}}
			if got := diagnosticpolicy.FilterInline([]byte(source), findings, nil); len(got) != 2 {
				t.Fatalf("unrelated prefix suppressed body: %q: %+v", source, got)
			}
		}
	}
}
