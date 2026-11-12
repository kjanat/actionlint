package matrix

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// ContextType derives the matrix context from reachable values. rowElementTypes
// supplies types inferred at row-value source positions within this resolution.
// It may be nil. Unknown includes can add values and names, so they prevent this
// fallback from narrowing the context. Unknown excludes only remove rows.
// No expressions are parsed or evaluated and no findings are produced here.
func (r Resolution) ContextType(rowElementTypes map[sourcepos.Position]exprtypes.Type) exprtypes.ObjectType {
	if !r.IncludesComplete() || r.Open() {
		rowElementTypes = nil
	}
	var object exprtypes.ObjectBuilder
	for name := range r.ReachableNames() {
		reach := r.ReachableAfterExcludes(name)
		ty := exprtypes.AnyType()
		first := true
		for _, candidate := range reach.Values {
			// Only row declarations supply fallback types. An unknown include
			// assignment must not acquire an axis's more specific element type.
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
