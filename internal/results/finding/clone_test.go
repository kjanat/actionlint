package finding

import (
	"bytes"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/binding"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/results"

	"github.com/google/go-cmp/cmp"
)

// referenceFinding sets every reference field of a finding.
func referenceFinding() *Finding {
	return &Finding{
		Reference: &sourcetext.Reference{Document: "doc", Node: "node"},
		Related:   []results.Related{{Message: "related"}},
		Fixes: []results.Fix{{
			Revisions: map[string]string{"doc": "rev"},
			Edits:     []results.Edit{{Replacement: "fixed"}},
		}},
		Rule:              "rule",
		Message:           "message",
		End:               sourcetext.Position{Line: 1, Column: 2},
		Source:            results.DiagnosticSource{Content: []byte("source"), Suppression: &results.SuppressionOrigin{Path: "target", Lines: sourcetext.LineRange{First: 1, Last: 2}}},
		SuppressionOrigin: &results.SuppressionOrigin{Path: "path"},
		IgnoreOrigin:      &IgnoreOrigin{StepPosition: &sourcetext.Position{Line: 3}, Job: "job"},
		Owner:             &Owner{Job: "job", Step: &sourcetext.Position{Line: 4}},
		MetadataIdentity:  &MetadataIdentity{Visit: 5},
	}
}

var cloneComparison = cmp.AllowUnexported(results.UnavailableSource{}, sourcetext.Revision{})

func TestCloneDetachesEveryReference(t *testing.T) {
	t.Parallel()
	original, want := referenceFinding(), referenceFinding()
	clone := original.Clone()
	if diff := cmp.Diff(want, clone, cloneComparison); diff != "" {
		t.Fatalf("clone differs from original:\n%s", diff)
	}

	clone.Reference.Node = "changed"
	clone.Related[0].Message = "changed"
	clone.Fixes[0].Revisions["doc"] = "changed"
	clone.Fixes[0].Edits[0].Replacement = "changed"
	clone.Source.Content[0] = 'S'
	clone.SuppressionOrigin.Path = "changed"
	clone.Source.Suppression.Path = "changed"
	clone.IgnoreOrigin.StepPosition.Line = 99
	clone.Owner.Step.Line = 99
	clone.MetadataIdentity.Visit = 99
	if diff := cmp.Diff(want, original, cloneComparison); diff != "" {
		t.Fatalf("annotating the clone changed the original:\n%s", diff)
	}
}

func TestClonePreservesAbsentReferences(t *testing.T) {
	t.Parallel()
	original := At(&sourcetext.Position{Line: 1, Column: 1}, "rule", "message")
	if diff := cmp.Diff(original, original.Clone(), cloneComparison); diff != "" {
		t.Fatalf("clone of a plain finding differs:\n%s", diff)
	}
}

func TestCloneDetachesLifecycleAndKeepsBindingIdentity(t *testing.T) {
	t.Parallel()
	namespace := binding.NewNamespace()
	original := &Finding{
		Binding:           namespace.ID(1),
		Lifecycle:         &results.LifecycleReport{Policies: []results.LifecyclePolicyReport{{ID: "original"}}},
		LifecycleEvidence: &lifecycle.Evidence{AllOf: []lifecycle.Evidence{{Name: "outer", AnyOf: []lifecycle.Evidence{{Name: "inner"}}}}},
	}
	clone := original.Clone()
	if index, ok := namespace.Index(clone.Binding); !ok || index != 1 {
		t.Fatalf("clone lost its binding identity: %v", clone.Binding)
	}
	clone.Lifecycle.Policies[0].ID = "changed"
	clone.LifecycleEvidence.AllOf[0].Name = "changed"
	clone.LifecycleEvidence.AllOf[0].AnyOf[0].Name = "changed"
	if original.Lifecycle.Policies[0].ID != "original" || original.LifecycleEvidence.AllOf[0].Name != "outer" || original.LifecycleEvidence.AllOf[0].AnyOf[0].Name != "inner" {
		t.Fatalf("clone changed original lifecycle data: %+v %+v", original.Lifecycle, original.LifecycleEvidence)
	}
}

func TestClonePreservesLifecycleSlicePresence(t *testing.T) {
	t.Parallel()
	original := &Finding{
		Lifecycle:         &results.LifecycleReport{Policies: []results.LifecyclePolicyReport{}},
		LifecycleEvidence: &lifecycle.Evidence{AllOf: []lifecycle.Evidence{}},
	}
	clone := original.Clone()
	if clone.Lifecycle.Policies == nil || clone.LifecycleEvidence.AllOf == nil || clone.LifecycleEvidence.AnyOf != nil {
		t.Fatalf("slice presence changed: %+v %+v", clone.Lifecycle, clone.LifecycleEvidence)
	}
}

func TestCopierSharesDetachedSourceButNotFindingMetadata(t *testing.T) {
	t.Parallel()
	buffer := bytes.Repeat([]byte("x"), 64<<10)
	original := &Finding{
		Source:    results.DiagnosticSource{Content: buffer},
		Reference: &sourcetext.Reference{Document: "original"},
		Lifecycle: &results.LifecycleReport{Policies: []results.LifecyclePolicyReport{{ID: "original"}}},
	}
	copier := NewCopier()
	copies := make([]*Finding, 100)
	for i := range copies {
		copies[i] = copier.Clone(original)
		if copies[i] == original || i > 0 && copies[i] == copies[0] {
			t.Fatalf("copy %d shares a mutable finding", i)
		}
		if &copies[i].Source.Content[0] == &buffer[0] || &copies[i].Source.Content[0] != &copies[0].Source.Content[0] {
			t.Fatalf("copy %d does not share the detached source buffer", i)
		}
	}
	copies[0].Reference.Document = "changed"
	copies[0].Lifecycle.Policies[0].ID = "changed"
	buffer[0] = 'y'
	if original.Reference.Document != "original" || copies[1].Reference.Document != "original" || original.Lifecycle.Policies[0].ID != "original" || copies[1].Lifecycle.Policies[0].ID != "original" || copies[0].Source.Content[0] != 'x' {
		t.Fatal("copying shared mutable metadata or retained the original source")
	}
	independent := original.Clone()
	if &independent.Source.Content[0] == &copies[0].Source.Content[0] {
		t.Fatal("a separate Clone shared another copier's source buffer")
	}
}
