package results_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestEveryRuleIsSuppressible(t *testing.T) {
	t.Parallel()
	for _, rule := range checking.ConfigurationRules().RuleInfos() {
		t.Run(rule.Name, func(t *testing.T) {
			t.Parallel()
			for _, severity := range []string{"error", "warning", "info"} {
				source := []byte("name: demo # actionlint:ignore " + rule.Name + " -- reviewed\n")
				findings := []*findingmodel.Finding{{Start: sourcepos.Position{Line: 1, Column: 1}, Rule: rule.Name, Severity: severity}}
				if got := (policy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline(source, findings, nil); len(got) != 0 {
					t.Fatal(got)
				}
			}
			cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {disable: [" + rule.Name + "]}}"))
			if err != nil || len(cfg.Lint.Rules.Disable) != 1 {
				t.Fatalf("%+v, %v", cfg, err)
			}
		})
	}
}

func TestForeignInlineSuppressionSource(t *testing.T) {
	t.Parallel()
	source := []byte("name: demo # actionlint:ignore shellcheck -- reviewed\n")
	finding := &findingmodel.Finding{Start: sourcepos.Position{Line: 1, Column: 1}, Rule: "shellcheck", Path: "local/action.yml", Source: source}
	if got := (policy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline(source, []*findingmodel.Finding{finding}, nil); len(got) != 1 {
		t.Fatal("caller suppressed foreign finding")
	}
	if got := (policy.Processor{RuleSet: checking.ConfigurationRules()}).FilterForeignInline(finding.Path, source, []*findingmodel.Finding{finding}, nil); len(got) != 0 {
		t.Fatal(got)
	}
	if finding.Source == nil {
		t.Fatal("modified original finding")
	}
}
