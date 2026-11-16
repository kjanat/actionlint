package policy_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestForeignSuppressionPreservesDiagnosticPath(t *testing.T) {
	t.Parallel()
	source := []byte("name: test # actionlint:ignore action --\n")
	item := &finding.Finding{Path: "./local/action.yml", Start: sourcetext.Position{Line: 2}, Rule: "expression", Source: results.DiagnosticSource{Content: source}}
	got := fixtureForeign(t, "/repo/local/action.yml", source, []*finding.Finding{item}, nil)
	if len(got) != 2 || got[0].Path != item.Path || got[1].Path != "/repo/local/action.yml" || got[1].Rule != "inline-suppression" {
		t.Fatalf("foreign filtering changed diagnostic identity: %+v", got)
	}
	if got[0] == item || string(got[0].Source.Content) != string(source) || item.Path != "./local/action.yml" {
		t.Fatalf("foreign filtering mutated the original finding: %+v", item)
	}
}
