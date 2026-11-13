package finding

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Canonical promises that its result shares no storage with the finding, so
// writing through every reference type in the result must leave the finding
// as it was.
func TestCanonicalSharesNoStorageWithTheFinding(t *testing.T) {
	t.Parallel()
	f := &Finding{
		Path:      "workflow.yml",
		Rule:      "rule",
		Start:     sourcepos.Position{Line: 1, Column: 1},
		Reference: &sourcepos.Reference{Document: "doc", Revision: "rev"},
		Related:   []results.Related{{Message: "related", Path: "other.yml"}},
		Fixes: []results.Fix{{
			Revisions: map[string]string{"workflow.yml": "sha"},
			Edits:     []results.Edit{{Path: "workflow.yml", Replacement: "new"}},
		}},
	}

	d := f.Canonical([]byte("on: push\n"))
	d.Reference.Document = "changed"
	d.Related[0].Message = "changed"
	d.Fixes[0].Revisions["workflow.yml"] = "changed"
	d.Fixes[0].Edits[0].Replacement = "changed"

	if f.Reference.Document != "doc" {
		t.Errorf("Reference shared: %q", f.Reference.Document)
	}
	if f.Related[0].Message != "related" {
		t.Errorf("Related shared: %q", f.Related[0].Message)
	}
	if f.Fixes[0].Revisions["workflow.yml"] != "sha" {
		t.Errorf("fix Revisions shared: %q", f.Fixes[0].Revisions["workflow.yml"])
	}
	if f.Fixes[0].Edits[0].Replacement != "new" {
		t.Errorf("fix Edits shared: %q", f.Fixes[0].Edits[0].Replacement)
	}
}

func TestForeignSelectsTheFindingsOwnSource(t *testing.T) {
	t.Parallel()
	workflow := []byte("workflow")
	for _, tc := range []struct {
		name    string
		own     []byte
		snap    []byte
		foreign bool
		want    string
	}{
		{"own source with snapshot", []byte("script"), workflow, true, "script"},
		{"no own source", nil, workflow, false, "workflow"},
		{"no snapshot", []byte("script"), nil, false, ""},
	} {
		f := &Finding{Source: tc.own}
		if got := f.Foreign(tc.snap); got != tc.foreign {
			t.Errorf("%s: Foreign = %v, want %v", tc.name, got, tc.foreign)
		}
		if got := string(f.sourceFor(tc.snap)); got != tc.want {
			t.Errorf("%s: sourceFor = %q, want %q", tc.name, got, tc.want)
		}
	}
}
