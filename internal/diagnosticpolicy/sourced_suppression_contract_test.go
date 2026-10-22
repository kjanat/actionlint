package diagnosticpolicy_test

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/internal/finding"
)

func TestForeignSuppressionPreservesDiagnosticPath(t *testing.T) {
	source := []byte("name: test # actionlint:ignore action --\n")
	finding := &findingmodel.Finding{Path: "./local/action.yml", Start: diagmodel.Position{Line: 2}, Rule: "expression", Source: source}
	got := diagnosticpolicy.FilterForeignInline("/repo/local/action.yml", source, []*findingmodel.Finding{finding}, nil)
	if len(got) != 2 || got[0].Path != finding.Path || got[1].Path != "/repo/local/action.yml" || got[1].Rule != "inline-suppression" {
		t.Fatalf("foreign filtering changed diagnostic identity: %+v", got)
	}
	if got[0] == finding || string(got[0].Source) != string(source) || finding.Path != "./local/action.yml" {
		t.Fatalf("foreign filtering mutated the original finding: %+v", finding)
	}
}
