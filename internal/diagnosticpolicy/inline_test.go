package diagnosticpolicy

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
)

func TestEveryRuleIsSuppressible(t *testing.T) {
	for _, rule := range repoconfig.RuleInfos() {
		t.Run(rule.Name, func(t *testing.T) {
			for _, severity := range []string{"error", "warning", "info"} {
				source := []byte("name: demo # actionlint:ignore " + rule.Name + " -- reviewed\n")
				findings := []*findingmodel.Finding{{Start: diagmodel.Position{Line: 1, Column: 1}, Rule: rule.Name, Severity: severity}}
				if got := FilterInline(source, findings, nil); len(got) != 0 {
					t.Fatal(got)
				}
			}
			cfg, err := repoconfig.Parse([]byte("lint: {rules: {disable: [" + rule.Name + "]}}"))
			if err != nil || len(cfg.Lint.Rules.Disable) != 1 {
				t.Fatalf("%+v, %v", cfg, err)
			}
		})
	}
}

func TestForeignInlineSuppressionSource(t *testing.T) {
	source := []byte("name: demo # actionlint:ignore shellcheck -- reviewed\n")
	finding := &findingmodel.Finding{Start: diagmodel.Position{Line: 1, Column: 1}, Rule: "shellcheck", Path: "local/action.yml", Source: source}
	if got := FilterInline(source, []*findingmodel.Finding{finding}, nil); len(got) != 1 {
		t.Fatal("caller suppressed foreign finding")
	}
	if got := FilterForeignInline(finding.Path, source, []*findingmodel.Finding{finding}, nil); len(got) != 0 {
		t.Fatal(got)
	}
	if finding.Source == nil {
		t.Fatal("modified original finding")
	}
}
