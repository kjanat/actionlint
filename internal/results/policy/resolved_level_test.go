package policy_test

import (
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestResultsUseResolvedDefaultSeverity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ level, reported, severity string }{
		{"info", "error", "info"},
		{"warn", "error", "warning"},
		{"error", "info", "error"},
		{"", "info", "info"},
	} {
		t.Run(test.level, func(t *testing.T) {
			t.Parallel()
			rules, err := repoconfig.NewRuleSet([]rule.Declaration{{Name: "example", Category: "correctness", Maturity: "stable", Recommended: true, DefaultLevel: test.level}})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := rules.Parse([]byte("lint: {rules: {correctness: {example: on}}}"))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = rules.ResolveRules(repoconfig.Presets{}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			processor := policy.Processor{RuleSet: rules}
			findings, err := processor.Apply(policy.Policy{Path: "workflow.yml", Config: cfg}, []*findingmodel.Finding{{Rule: "example", Severity: test.reported, Message: "example"}}, nil, nil)
			if err != nil || len(findings) != 1 || findings[0].Severity != test.severity {
				t.Fatalf("findings=%+v, error=%v; want severity %s", findings, err, test.severity)
			}
		})
	}
}
