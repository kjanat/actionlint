package policy_test

import (
	"fmt"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	resultpolicy "actionlint.kjanat.dev/v2/internal/results/policy"

	"github.com/google/go-cmp/cmp"
)

func TestInlineSuppressionBlockBody(t *testing.T) {
	t.Parallel()
	for _, header := range []string{
		"run: | # actionlint:ignore expression-reference shellcheck -- reviewed",
		"run: | # actionlint-ignore expression-reference, shellcheck -- reviewed",
		"run: | # ignore[expression-reference shellcheck] -- reviewed",
		"# actionlint-ignore-next-line expression-reference shellcheck -- reviewed\nrun: |",
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
				t.Parallel()
				start := strings.Count(header, "\n") + 1
				source := strings.ReplaceAll(header+`
    echo first
    echo second
next: value
`, "\n", ending)
				findings := []*findingmodel.Finding{
					{Start: sourcepos.Position{Line: start + 1}, Rule: "expression-reference"},
					{Start: sourcepos.Position{Line: start + 2}, Rule: "shellcheck"},
					{Start: sourcepos.Position{Line: start + 2}, Rule: "action"},
					{Start: sourcepos.Position{Line: start + 3}, Rule: "expression-reference"},
				}
				got := (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline([]byte(source), findings, nil)
				if diff := cmp.Diff(findings[2:], got, cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestInlineSuppressionSpellingsRespectPolicy(t *testing.T) {
	t.Parallel()
	cfg, err := checking.ConfigurationRules().Parse([]byte("policy: {disallow-suppressions: {rules: [expression-reference], report: all}}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range []string{
		"actionlint:ignore expression-reference shellcheck",
		"actionlint-ignore expression-reference,shellcheck",
		"ignore[expression-reference shellcheck]",
	} {
		t.Run(declaration, func(t *testing.T) {
			t.Parallel()
			source := []byte("run: | # " + declaration + "\n  echo ok\nnext: value\n")
			findings := []*findingmodel.Finding{
				{Start: sourcepos.Position{Line: 2}, Rule: "expression-reference"},
				{Start: sourcepos.Position{Line: 2}, Rule: "shellcheck"},
				{Start: sourcepos.Position{Line: 3}, Rule: "expression-reference"},
			}
			got := (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline(source, findings, cfg.Policy.DisallowSuppressions)
			if len(got) != 3 || got[0] != findings[0] || got[1] != findings[2] || got[2].Rule != "forbidden-suppression" || got[2].Start.Line != 1 {
				t.Fatalf("directive bypassed policy or changed its extent: %+v", got)
			}
		})
	}
}

func TestInlineSuppressionRegistry(t *testing.T) {
	t.Parallel()
	for _, name := range (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).SuppressionRules() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().Parse(fmt.Appendf(nil, "policy: {disallow-suppressions: {rules: [%s]}}", name))
			if err != nil {
				t.Fatal(err)
			}
			source := []byte("first: value\nsecond: value\nthird: value\n# actionlint:ignore-next-line " + name + " -- reviewed\nfifth: value\n")
			finding := &findingmodel.Finding{Start: sourcepos.Position{Line: 5}, Rule: name}
			if got := (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline(source, []*findingmodel.Finding{finding}, nil); len(got) != 0 {
				t.Fatalf("registered rule was not suppressed: %v", got)
			}
			got := (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline(source, []*findingmodel.Finding{finding}, cfg.Policy.DisallowSuppressions)
			if len(got) != 2 || got[0] != finding || got[1].Rule != "forbidden-suppression" || got[1].Start.Line != 4 {
				t.Fatalf("restriction did not retain finding and report comment: %v", got)
			}
		})
	}
}

func TestInlineSuppressionScriptTextIsNotDirective(t *testing.T) {
	t.Parallel()
	source := []byte(`run: |
  text = '''
  # actionlint:ignore-next-line expression-reference -- sample text
  ${{ github.event.issue.title }}
  '''
`)
	finding := &findingmodel.Finding{Start: sourcepos.Position{Line: 4}, Rule: "expression-reference"}
	got := (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline(source, []*findingmodel.Finding{finding}, nil)
	if len(got) != 1 || got[0] != finding {
		t.Fatalf("script text changed findings: %+v", got)
	}
}

func TestSuppressionTargetTracksAllowedRulesAndOrigins(t *testing.T) {
	t.Parallel()
	policy, err := checking.ConfigurationRules().DisallowSuppressions("all", "shellcheck")
	if err != nil {
		t.Fatal(err)
	}
	findings := []*findingmodel.Finding{
		{Rule: "expression-reference", Start: sourcepos.Position{Line: 4}},
		{Rule: "shellcheck", Start: sourcepos.Position{Line: 7}},
		{Rule: "plugin-rule", Start: sourcepos.Position{Line: 9}},
		{Rule: "inline-suppression", Start: sourcepos.Position{Line: 10}},
		{Rule: "forbidden-suppression", Start: sourcepos.Position{Line: 11}},
	}
	got := (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline([]byte("workflow"), findings, policy, "plugin-rule")
	want := &findingmodel.SuppressionOrigin{First: 4, Last: 4}
	if len(got) != len(findings) || got[0].SuppressionTarget == nil || *got[0].SuppressionTarget != *want {
		t.Fatalf("allowed rule target missing: %+v", got)
	}
	if got[2].SuppressionTarget == nil || got[2].SuppressionTarget.First != 9 || got[2].SuppressionTarget.Last != 9 {
		t.Fatalf("plugin rule target missing: %+v", got[2])
	}
	for _, index := range []int{1, 3, 4} {
		if got[index].SuppressionTarget != nil {
			t.Fatalf("ineligible rule %q got target %+v", got[index].Rule, got[index].SuppressionTarget)
		}
	}

	foreign := &findingmodel.Finding{Rule: "shellcheck", Path: "script.sh", Start: sourcepos.Position{Line: 2}, Source: []byte("echo hi")}
	origin := &findingmodel.Finding{Rule: "shellcheck", Path: "script.sh", Start: sourcepos.Position{Line: 2}, Source: []byte("echo hi"), SuppressionOrigin: &findingmodel.SuppressionOrigin{Path: "action.yml", First: 12, Last: 14}}
	got = (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).FilterForeignInline("action.yml", []byte("run: echo hi"), []*findingmodel.Finding{foreign, origin}, nil)
	if len(got) != 2 || got[0].SuppressionTarget == nil || *got[0].SuppressionTarget != (findingmodel.SuppressionOrigin{Path: "action.yml", First: 2, Last: 2}) || got[1].SuppressionTarget == nil || *got[1].SuppressionTarget != *origin.SuppressionOrigin {
		t.Fatalf("foreign targets lost declaration scope: %+v", got)
	}
}

func TestInlineSuppressionSplitPrefixIsolation(t *testing.T) {
	t.Parallel()
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
			findings := []*findingmodel.Finding{{Start: sourcepos.Position{Line: line}, Rule: "expression-reference"}, {Start: sourcepos.Position{Line: line}, Rule: "shellcheck"}}
			if got := (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline([]byte(source), findings, nil); len(got) != 2 {
				t.Fatalf("unrelated prefix suppressed body: %q: %+v", source, got)
			}
		}
	}
}
