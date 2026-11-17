package githubaction

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestWorkspaceReportRebasesRelatedLocationsAndFixRevisions(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	revision, err := sourcetext.NewRevision(filepath.ToSlash(filepath.Join(workspace, "subdir", "other.yml")), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	analysis := &report.Analysis{Diagnostics: []results.PublishedDiagnostic{{
		Path:    "ci.yml",
		Related: []results.Related{{Path: "other.yml", Message: "related declaration", Revision: revision, Coordinates: sourcetext.YAML}},
		Fixes:   []results.Fix{{Revisions: map[string]string{"other.yml": "revision"}, Edits: []results.Edit{{Path: "other.yml"}}}},
	}}}
	reported := workspaceReportAnalysis(analysis, filepath.Join(workspace, "subdir"), workspace)
	d := reported.Diagnostics[0]
	if d.Related[0].Revision != revision || d.Related[0].Coordinates != sourcetext.YAML {
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
