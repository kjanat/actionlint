package lsp_test

import (
	"path/filepath"
	"testing"

	reportfixture "actionlint.kjanat.dev/v2/internal/development/tests/support/reportfixture"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/lsp"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestEditorDiagnosticsOwnTheirReportData(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yml")
	captured := reportfixture.CapturedSource(t, path, []byte("on: push\n"), source.YAML)
	input := &report.Analysis{
		Diagnostics: []results.PublishedDiagnostic{{
			Rule: "example", Path: "workflow.yml", Start: source.Position{Line: 1, Column: 1}, End: source.Position{Line: 1, Column: 2},
			Reference: &source.Reference{Document: captured.Revision.Document(), Revision: captured.Revision.Digest()},
			Lifecycle: &results.LifecycleReport{Policies: []results.LifecyclePolicyReport{{ID: "original", Origin: "required.toml", Enforced: true}}},
			Fixes: []results.Fix{{
				Description: "Example edit", Revisions: map[string]string{"workflow.yml": captured.Revision.Digest()},
				Edits: []results.Edit{{Path: "workflow.yml", Replacement: "replacement"}},
			}}, Source: results.DiagnosticSource{Content: captured.Content},
		}},
		CapturedSources: map[string]report.CapturedSource{path: captured},
	}
	analysis, err := lsp.FromReport(input, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Diagnostics) != 1 {
		t.Fatalf("expected one editor diagnostic, got %d", len(analysis.Diagnostics))
	}
	output := &analysis.Diagnostics[0]
	output.Lifecycle.Policies[0].Origin = "changed"
	output.Reference.Node = "changed"
	output.Fixes[0].Edits[0].Replacement = "changed"
	if got := input.Diagnostics[0].Lifecycle.Policies[0].Origin; got != "required.toml" {
		t.Errorf("editor changed the analysis policy origin to %q, want required.toml", got)
	}
	if got := input.Diagnostics[0].Reference.Node; got != "" {
		t.Errorf("editor changed the analysis reference node to %q, want empty", got)
	}
	if got := input.Diagnostics[0].Fixes[0].Edits[0].Replacement; got != "replacement" {
		t.Errorf("editor changed the analysis replacement to %q, want replacement", got)
	}
}
