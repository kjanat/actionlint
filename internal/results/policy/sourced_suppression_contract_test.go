package policy_test

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestForeignSuppressionPreservesDiagnosticPath(t *testing.T) {
	t.Parallel()
	source := []byte("name: test # actionlint:ignore action --\n")
	finding := &findingmodel.Finding{Path: "./local/action.yml", Start: sourcepos.Position{Line: 2}, Rule: "expression", Source: results.DiagnosticSource{Content: source}}
	got := fixtureForeign(t, "/repo/local/action.yml", source, []*findingmodel.Finding{finding}, nil)
	if len(got) != 2 || got[0].Path != finding.Path || got[1].Path != "/repo/local/action.yml" || got[1].Rule != "inline-suppression" {
		t.Fatalf("foreign filtering changed diagnostic identity: %+v", got)
	}
	if got[0] == finding || string(got[0].Source.Content) != string(source) || finding.Path != "./local/action.yml" {
		t.Fatalf("foreign filtering mutated the original finding: %+v", finding)
	}
}
