package policy

import (
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestIgnoreSelectors(t *testing.T) {
	t.Parallel()
	rules, err := repoconfig.NewRuleSet([]rule.Declaration{{Name: "shellcheck", Category: "correctness", Maturity: "stable", Recommended: true, Aliases: []rule.Alias{{Name: "old-shellcheck"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		selector, name, code string
		ignored              bool
	}{
		{"shellcheck", "shellcheck", "SC2086", true},
		{"old-shellcheck", "shellcheck", "SC2086", true},
		{"SC2086", "shellcheck", "SC2086", true},
		{"SC2086", "another-plugin", "SC2086", true},
		{"shellcheck/SC2086", "shellcheck", "SC2086", true},
		{"old-shellcheck/SC2086", "shellcheck", "SC2086", true},
		{"shellcheck/SC2086", "another-plugin", "SC2086", false},
		{"shellcheck/SC2086", "shellcheck", "SC2016", false},
		{"vendor/check", "another-plugin", "vendor/check", true},
		{"shellcheck/vendor/check", "shellcheck", "vendor/check", true},
		{"vendor/check", "vendor/check", "", true},
		{"SC208", "shellcheck", "SC2086", false},
		{"SC2086", "shellcheck", "", false},
		{"shell.*", "shellcheck", "SC2086", false},
	} {
		t.Run(tc.selector+"/"+tc.name+"/"+tc.code, func(t *testing.T) {
			t.Parallel()
			selectors, err := CompileIgnoreSelectors([]string{tc.selector, tc.selector})
			if err != nil {
				t.Fatal(err)
			}
			findings, err := (Processor{RuleSet: rules}).Apply(Policy{Path: "workflow.yml", IgnoreSelectors: selectors}, []*findingmodel.Finding{{Rule: tc.name, Code: tc.code, Message: "some message"}}, nil, nil)
			if err != nil || (len(findings) == 0) != tc.ignored {
				t.Fatalf("findings=%+v err=%v, want ignored=%t", findings, err, tc.ignored)
			}
		})
	}
}

func TestInvalidIgnoreSelectors(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", " ", "a b", "a,b", "/code", "rule/"} {
		if _, err := CompileIgnoreSelectors([]string{value}); err == nil {
			t.Errorf("accepted invalid selector %q", value)
		}
	}
}
