package finding

import (
	"bytes"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/source"
)

// Clone returns a deep copy of the finding. Annotating the copy, including its
// origins, fixes and foreign source, leaves the original unchanged.
func (e *Finding) Clone() *Finding {
	clone := e.detached()
	return &clone
}

// detached copies the value fields and gives every reference field fresh
// storage. A new reference field must be detached here as well.
func (e *Finding) detached() Finding {
	out := *e
	out.Diagnostic = detachedDiagnostic(e.Diagnostic)
	out.EndPosition = clonePos(e.EndPosition)
	out.Source = bytes.Clone(e.Source)
	out.SuppressionOrigin = cloneSuppressionOrigin(e.SuppressionOrigin)
	out.IgnoreOrigin = cloneIgnoreOrigin(e.IgnoreOrigin)
	out.Owner = cloneOwner(e.Owner)
	out.MetadataIdentity = cloneMetadataIdentity(e.MetadataIdentity)
	return out
}

func detachedDiagnostic(d diagnostic.Diagnostic) diagnostic.Diagnostic {
	d.Reference = cloneReference(d.Reference)
	d.Related = slices.Clone(d.Related)
	d.Fixes = cloneFixes(d.Fixes)
	return d
}

func cloneFixes(fixes []diagnostic.Fix) []diagnostic.Fix {
	if fixes == nil {
		return nil
	}
	out := make([]diagnostic.Fix, 0, len(fixes))
	for _, fix := range fixes {
		out = append(out, detachedFix(fix))
	}
	return out
}

func detachedFix(fix diagnostic.Fix) diagnostic.Fix {
	fix.Revisions = maps.Clone(fix.Revisions)
	fix.Edits = slices.Clone(fix.Edits)
	return fix
}

func detachedOwner(owner Owner) Owner {
	owner.Step = clonePos(owner.Step)
	return owner
}

func cloneOwner(owner *Owner) *Owner {
	if owner == nil {
		return nil
	}
	clone := detachedOwner(*owner)
	return &clone
}

func detachedIgnoreOrigin(origin IgnoreOrigin) IgnoreOrigin {
	origin.StepPosition = clonePos(origin.StepPosition)
	return origin
}

func cloneIgnoreOrigin(origin *IgnoreOrigin) *IgnoreOrigin {
	if origin == nil {
		return nil
	}
	clone := detachedIgnoreOrigin(*origin)
	return &clone
}

// The following types hold only plain values, so a value copy detaches them.

func clonePos(pos *workflowlang.Pos) *workflowlang.Pos {
	if pos == nil {
		return nil
	}
	return new(*pos)
}

func cloneReference(reference *source.Reference) *source.Reference {
	if reference == nil {
		return nil
	}
	return new(*reference)
}

func cloneSuppressionOrigin(origin *SuppressionOrigin) *SuppressionOrigin {
	if origin == nil {
		return nil
	}
	return new(*origin)
}

func cloneMetadataIdentity(identity *MetadataIdentity) *MetadataIdentity {
	if identity == nil {
		return nil
	}
	return new(*identity)
}
