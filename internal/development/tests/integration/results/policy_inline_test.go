package results_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestEveryRuleIsSuppressible(t *testing.T) {
	t.Parallel()
	for _, rule := range checking.ConfigurationRules().RuleInfos() {
		t.Run(rule.Name, func(t *testing.T) {
			t.Parallel()
			for _, level := range []severity.Severity{severity.Error, severity.Warning, severity.Info} {
				source := []byte("name: demo # actionlint:ignore " + rule.Name + " -- reviewed\n")
				findings := []*finding.Finding{{Start: sourcetext.Position{Line: 1, Column: 1}, Rule: rule.Name, Severity: level}}
				disabled := "disable: [unused-suppression], "
				if rule.Name == "unused-suppression" {
					disabled = ""
				}
				cfg, err := checking.ConfigurationRules().ParseConfig(fmt.Appendf(nil, "lint: {rules: {%s%s: {%s: on}}}", disabled, rule.Category, rule.Name))
				if err != nil {
					t.Fatal(err)
				}
				got, err := publishFindings(t, policy.Policy{Path: "workflow.yml", ConfigPath: "workflow.yml", Content: source}, cfg, findings, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if rule.Name == "forbidden-suppression" {
					if len(got) != 2 || got[0].Rule != "forbidden-suppression" || got[1].Rule != "forbidden-suppression" {
						t.Fatalf("forbidden suppression was not reported: %+v", got)
					}
				} else if len(got) != 0 {
					t.Fatalf("findings=%+v", got)
				}
			}
			cfg, err := checking.ConfigurationRules().ParseConfig([]byte("lint: {rules: {disable: [" + rule.Name + "]}}"))
			if err != nil || len(cfg.Lint.Rules.Disable) != 1 {
				t.Fatalf("%+v, %v", cfg, err)
			}
		})
	}
}

func TestForeignInlineSuppressionSource(t *testing.T) {
	t.Parallel()
	source := []byte("name: demo # actionlint:ignore shellcheck -- reviewed\n")
	item := &finding.Finding{Start: sourcetext.Position{Line: 1, Column: 1}, Rule: "shellcheck", Path: "local/action.yml", Source: results.DiagnosticSource{Content: source}}
	cfg, err := checking.ConfigurationRules().ParseConfig([]byte("lint: {rules: {disable: [unused-suppression], correctness: {shellcheck: on}}}"))
	if err != nil {
		t.Fatal(err)
	}
	p := policy.Policy{Path: "workflow.yml", ConfigPath: "workflow.yml", Content: source}
	got, err := publishFindings(t, p, cfg, []*finding.Finding{item}, nil, nil)
	if err != nil || len(got) != 1 || got[0].Rule != "shellcheck" {
		t.Fatalf("caller suppressed foreign finding: %+v %v", got, err)
	}
	got, err = publishFindings(t, p, cfg, []*finding.Finding{item}, map[string][]byte{item.Path: source}, map[string]*configuration.Config{item.Path: cfg})
	if err != nil || len(got) != 0 {
		t.Fatalf("foreign suppression: %+v %v", got, err)
	}
	if item.Source.Content == nil {
		t.Fatal("modified original finding")
	}
}
