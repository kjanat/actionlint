package results

import "testing"

func TestFixCloneOwnsEditsAndRevisions(t *testing.T) {
	t.Parallel()
	original := Fix{
		Applicability: FixSafe,
		Revisions:     map[string]string{"a.yml": "revision"},
		Edits:         []Edit{{Path: "a.yml", Replacement: "first"}, {Path: "a.yml", Replacement: "second"}},
	}
	clone := original.Clone()
	clone.Revisions["a.yml"] = "changed"
	clone.Edits[0].Replacement = "changed"
	clone.Applicability = FixUnsafe
	if original.Revisions["a.yml"] != "revision" || original.Edits[0].Replacement != "first" || original.Edits[1].Replacement != "second" || original.Applicability != FixSafe {
		t.Fatalf("editing a copy changed the original: %+v", original)
	}
}
