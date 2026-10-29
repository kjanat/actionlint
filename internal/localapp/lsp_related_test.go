package localapp

import (
	"errors"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/langserver"
	"actionlint.kjanat.dev/v2/internal/report"
	"actionlint.kjanat.dev/v2/source"
)

func TestEditorRelatedRequiresCapturedRevisionModelAndRange(t *testing.T) {
	for _, name := range []string{"valid", "different revision", "different model", "outside source", "unbound", "zero end"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path, relatedPath := filepath.Join(root, "workflow.yml"), filepath.Join(root, "related.yml")
			main := capturedEditorSource(t, path, []byte("main\n"), source.YAML)
			target := capturedEditorSource(t, relatedPath, []byte("😀related\n"), source.YAML)
			related := diagnostic.Related{Revision: target.Revision, Coordinates: target.Coordinates, Path: "related.yml", Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 1, Column: 9}}
			switch name {
			case "different revision":
				related.Revision = capturedEditorSource(t, relatedPath, []byte("other bytes"), source.YAML).Revision
			case "different model":
				related.Coordinates = source.RawLF
			case "outside source":
				related.End.Column = 99
			case "unbound":
				related.Revision, related.Coordinates = source.Revision{}, ""
			case "zero end":
				related.End = diagnostic.Position{}
			}
			result := &application.Result{Diagnostics: []diagnostic.Diagnostic{{Path: "workflow.yml", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 2}, Reference: &source.Reference{Document: main.Revision.Document(), Revision: main.Revision.Digest()}, Related: []diagnostic.Related{related}}}, Sources: []report.DiagnosticSource{{Content: main.Content}}, CapturedSources: map[string]report.CapturedSource{path: main, relatedPath: target}}
			analysis, err := editorAnalysis(result, root, map[string]langserver.Document{path: {Text: string(main.Content)}})
			if len(analysis.Diagnostics) != 1 {
				t.Fatalf("related rejection lost primary diagnostic: %+v %v", analysis, err)
			}
			if name == "valid" {
				if err != nil || len(analysis.Diagnostics[0].Related) != 1 || analysis.Diagnostics[0].Related[0].Path != relatedPath {
					t.Fatalf("bound related source lost: %+v %v", analysis, err)
				}
			} else if _, unavailable := errors.AsType[*editorEvidenceError](err); !unavailable || len(analysis.Diagnostics[0].Related) != 0 {
				t.Fatalf("unverifiable related evidence was promoted by path: %+v %v", analysis, err)
			}
		})
	}
}
