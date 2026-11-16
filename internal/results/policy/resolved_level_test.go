package policy_test

import (
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
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
			rules, err := configuration.NewRuleSet([]rule.Declaration{{Name: "example", Category: "correctness", Maturity: "stable", Recommended: true, DefaultLevel: test.level}})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := rules.ParseConfig([]byte("lint: {rules: {correctness: {example: on}}}"))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = rules.ResolveRules(configuration.Presets{}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			processor := policy.Processor{RuleSet: rules}
			input := []*findingmodel.Finding{{Rule: "example", Severity: test.reported, Message: "example"}}
			p := preparePolicy(rules, cfg, policy.Policy{Path: "workflow.yml"}, nil, input)
			findings, err := publishFindings(processor, p, input, nil)
			if err != nil || len(findings) != 1 || findings[0].Severity != test.severity {
				t.Fatalf("findings=%+v, error=%v; want severity %s", findings, err, test.severity)
			}
		})
	}
}
