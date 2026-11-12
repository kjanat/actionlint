package lsp_test

import (
	"errors"
	"path/filepath"
	"testing"

	reportfixture "actionlint.kjanat.dev/v2/internal/development/tests/support/reportfixture"

	"actionlint.kjanat.dev/v2/internal/results"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/lsp"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestEditorRelatedRequiresCapturedRevisionModelAndRange(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"valid", "different revision", "different model", "outside source", "unbound", "zero end"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path, relatedPath := filepath.Join(root, "workflow.yml"), filepath.Join(root, "related.yml")
			main := reportfixture.CapturedSource(t, path, []byte("main\n"), sourcepos.YAML)
			target := reportfixture.CapturedSource(t, relatedPath, []byte("😀related\n"), sourcepos.YAML)
			related := results.Related{Revision: target.Revision, Coordinates: target.Coordinates, Path: "related.yml", Start: sourcepos.Position{Line: 1, Column: 2}, End: sourcepos.Position{Line: 1, Column: 9}}
			switch name {
			case "different revision":
				related.Revision = reportfixture.CapturedSource(t, relatedPath, []byte("other bytes"), sourcepos.YAML).Revision
			case "different model":
				related.Coordinates = sourcepos.RawLF
			case "outside source":
				related.End.Column = 99
			case "unbound":
				related.Revision, related.Coordinates = sourcepos.Revision{}, ""
			case "zero end":
				related.End = sourcepos.Position{}
			}
			result := &report.Analysis{Diagnostics: []results.Diagnostic{{Path: "workflow.yml", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 2}, Reference: &sourcepos.Reference{Document: main.Revision.Document(), Revision: main.Revision.Digest()}, Related: []results.Related{related}}}, Sources: []report.DiagnosticSource{{Content: main.Content}}, CapturedSources: map[string]report.CapturedSource{path: main, relatedPath: target}}
			analysis, err := lsp.FromReport(result, root, map[string]lsp.Document{path: {Text: string(main.Content)}})
			if len(analysis.Diagnostics) != 1 {
				t.Fatalf("related rejection lost primary diagnostic: %+v %v", analysis, err)
			}
			if name == "valid" {
				if err != nil || len(analysis.Diagnostics[0].Related) != 1 || analysis.Diagnostics[0].Related[0].Path != relatedPath {
					t.Fatalf("bound related source lost: %+v %v", analysis, err)
				}
			} else if _, unavailable := errors.AsType[*lsp.UnavailableSourcesError](err); !unavailable || len(analysis.Diagnostics[0].Related) != 0 {
				t.Fatalf("unverifiable related evidence was promoted by path: %+v %v", analysis, err)
			}
		})
	}
}
