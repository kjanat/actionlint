package results

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/applicability"
)

func TestFixCloneDoesNotAliasEditsAndRevisions(t *testing.T) {
	t.Parallel()
	original := Fix{
		Applicability: applicability.Safe,
		Revisions:     map[string]string{"a.yml": "revision"},
		Edits:         []Edit{{Path: "a.yml", Replacement: "first"}, {Path: "a.yml", Replacement: "second"}},
	}
	clone := original.Clone()
	clone.Revisions["a.yml"] = "changed"
	clone.Edits[0].Replacement = "changed"
	if original.Revisions["a.yml"] != "revision" || original.Edits[0].Replacement != "first" || original.Edits[1].Replacement != "second" || original.Applicability != applicability.Safe {
		t.Fatalf("editing a copy changed the original: %+v", original)
	}
}
