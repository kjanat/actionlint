package results

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

func TestDiagnosticCloneDetachesMutableFields(t *testing.T) {
	t.Parallel()
	original := Diagnostic{
		Rule: "example", Reference: &sourcetext.Reference{Document: "original"},
		Related:   []Related{{Message: "related"}},
		Lifecycle: &LifecycleReport{Policies: []LifecyclePolicyReport{{ID: "policy"}}},
		Fixes:     []Fix{{Revisions: map[string]string{"file": "revision"}, Edits: []Edit{{Replacement: "original"}}}},
	}
	cloned := original.Clone()
	cloned.Reference.Document = "changed"
	cloned.Related[0].Message = "changed"
	cloned.Lifecycle.Policies[0].ID = "changed"
	cloned.Fixes[0].Revisions["file"] = "changed"
	cloned.Fixes[0].Edits[0].Replacement = "changed"
	if original.Reference.Document != "original" || original.Related[0].Message != "related" || original.Lifecycle.Policies[0].ID != "policy" || original.Fixes[0].Revisions["file"] != "revision" || original.Fixes[0].Edits[0].Replacement != "original" {
		t.Fatalf("clone changed its source: %+v", original)
	}
	original.Lifecycle.Policies[0].ID = "second change"
	if cloned.Lifecycle.Policies[0].ID != "changed" {
		t.Fatalf("source changed clone: %+v", cloned.Lifecycle)
	}
}

func TestDiagnosticClonePreservesNilAndEmptySlices(t *testing.T) {
	t.Parallel()
	original := Diagnostic{Related: []Related{}, Lifecycle: &LifecycleReport{Policies: []LifecyclePolicyReport{}}}
	cloned := original.Clone()
	if cloned.Related == nil || cloned.Lifecycle.Policies == nil || cloned.Fixes != nil {
		t.Fatalf("clone changed slice presence: %+v", cloned)
	}
}
