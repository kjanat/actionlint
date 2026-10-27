package githubaction

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/source"
)

func TestWorkspaceReportRebasesRelatedLocationsAndFixRevisions(t *testing.T) {
	workspace := t.TempDir()
	revision, err := source.NewRevision(filepath.ToSlash(filepath.Join(workspace, "subdir", "other.yml")), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	analysis := &application.Result{Diagnostics: []diagnostic.Diagnostic{{Path: "ci.yml",
		Related: []diagnostic.Related{{Path: "other.yml", Message: "related declaration", Revision: revision, Coordinates: source.YAML}},
		Fixes:   []diagnostic.Fix{{Revisions: map[string]string{"other.yml": "revision"}, Edits: []diagnostic.Edit{{Path: "other.yml"}}}},
	}}}
	reported := workspaceReportAnalysis(analysis, filepath.Join(workspace, "subdir"), workspace)
	d := reported.Diagnostics[0]
	if d.Related[0].Revision != revision || d.Related[0].Coordinates != source.YAML {
		t.Fatal("path rewriting changed related binding")
	}
	if d.Path != "subdir/ci.yml" || d.Related[0].Path != "subdir/other.yml" || d.Fixes[0].Edits[0].Path != "subdir/other.yml" || d.Fixes[0].Revisions["subdir/other.yml"] != "revision" {
		t.Fatalf("report paths lost source bindings: %+v", d)
	}
	d.Related[0].Message = "changed"
	d.Fixes[0].Revisions["other.yml"] = "changed"
	if original := analysis.Diagnostics[0]; original.Path != "ci.yml" || original.Related[0].Path != "other.yml" || original.Related[0].Message != "related declaration" || original.Fixes[0].Revisions["other.yml"] != "revision" {
		t.Fatalf("report projection mutated persisted diagnostics: %+v", original)
	}
}
