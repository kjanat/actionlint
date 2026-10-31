package finding

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/source"

	"github.com/google/go-cmp/cmp"
)

// referenceFinding sets every reference field of a finding.
func referenceFinding() *Finding {
	return &Finding{
		Reference: &source.Reference{Document: "doc", Node: "node"},
		Related:   []diagmodel.Related{{Message: "related"}},
		Fixes: []diagmodel.Fix{{
			Revisions: map[string]string{"doc": "rev"},
			Edits:     []diagmodel.Edit{{Replacement: "fixed"}},
		}},
		Rule:              "rule",
		Message:           "message",
		EndPosition:       &workflowlang.Pos{Line: 1, Col: 2},
		Source:            []byte("source"),
		SuppressionOrigin: &SuppressionOrigin{Path: "path"},
		IgnoreOrigin:      &IgnoreOrigin{StepPosition: &workflowlang.Pos{Line: 3}, Job: "job"},
		Owner:             &Owner{Job: "job", Step: &workflowlang.Pos{Line: 4}},
		MetadataIdentity:  &MetadataIdentity{Visit: 5},
	}
}

var cloneComparison = cmp.AllowUnexported(UnavailableSource{}, source.Revision{})

func TestCloneDetachesEveryReference(t *testing.T) {
	original, want := referenceFinding(), referenceFinding()
	clone := original.Clone()
	if diff := cmp.Diff(want, clone, cloneComparison); diff != "" {
		t.Fatalf("clone differs from original:\n%s", diff)
	}

	clone.Reference.Node = "changed"
	clone.Related[0].Message = "changed"
	clone.Fixes[0].Revisions["doc"] = "changed"
	clone.Fixes[0].Edits[0].Replacement = "changed"
	clone.EndPosition.Line = 99
	clone.Source[0] = 'S'
	clone.SuppressionOrigin.Path = "changed"
	clone.IgnoreOrigin.StepPosition.Line = 99
	clone.Owner.Step.Line = 99
	clone.MetadataIdentity.Visit = 99
	if diff := cmp.Diff(want, original, cloneComparison); diff != "" {
		t.Fatalf("annotating the clone changed the original:\n%s", diff)
	}
}

func TestClonePreservesAbsentReferences(t *testing.T) {
	original := At(&workflowlang.Pos{Line: 1, Col: 1}, "rule", "message")
	if diff := cmp.Diff(original, original.Clone(), cloneComparison); diff != "" {
		t.Fatalf("clone of a plain finding differs:\n%s", diff)
	}
}
