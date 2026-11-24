package matrix

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
)

// ContextType parses no expressions and reports no findings, and rowElementTypes may be nil.
func (r Resolution) ContextType(rowElementTypes map[sourcetext.Position]exprtypes.Type) exprtypes.ObjectType {
	if !r.IncludesComplete() || r.Open() {
		rowElementTypes = nil
	}
	var object exprtypes.ObjectBuilder
	for name := range r.ReachableNames() {
		reach := r.ReachableAfterExcludes(name)
		ty := exprtypes.AnyType()
		first := true
		for _, candidate := range reach.Values {
			// An unknown include assignment must not acquire an axis's more specific element type.
			inferred := candidate.TypeWithFallback(rowElementTypes[candidate.Origin().Span.Start])
			if first {
				ty, first = inferred, false
			} else {
				ty = ty.Merge(inferred)
			}
		}
		if !r.IncludesComplete() {
			ty = exprtypes.AnyType()
		}
		object.Set(name, ty)
	}
	if r.Open() || !r.IncludesComplete() {
		object.SetMapped(exprtypes.AnyType())
	}
	return object.Build()
}
