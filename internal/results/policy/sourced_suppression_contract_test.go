package policy_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestForeignSuppressionPreservesDiagnosticPath(t *testing.T) {
	t.Parallel()
	source := []byte("name: test # actionlint:ignore action --\n")
	finding := &findingmodel.Finding{Path: "./local/action.yml", Start: sourcepos.Position{Line: 2}, Rule: "expression", Source: source}
	got := (policy.Processor{RuleSet: checking.ConfigurationRules()}).FilterForeignInline("/repo/local/action.yml", source, []*findingmodel.Finding{finding}, nil)
	if len(got) != 2 || got[0].Path != finding.Path || got[1].Path != "/repo/local/action.yml" || got[1].Rule != "inline-suppression" {
		t.Fatalf("foreign filtering changed diagnostic identity: %+v", got)
	}
	if got[0] == finding || string(got[0].Source) != string(source) || finding.Path != "./local/action.yml" {
		t.Fatalf("foreign filtering mutated the original finding: %+v", finding)
	}
}
