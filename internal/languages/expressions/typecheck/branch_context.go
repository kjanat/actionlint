package typecheck

import (
	"maps"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// withBranchContext checks a branch using model-supplied context refinements.
// Overrides apply only inside this operation, including nested branches.
func (sema *Checker) withBranchContext(guard exprsyntax.Node, truth bool, check func() exprtypes.Type) exprtypes.Type {
	overrides, possible := sema.narrowContext(guard, truth, sema.ContextType)
	if !possible {
		return exprtypes.AnyType()
	}
	if len(overrides) == 0 {
		return check()
	}
	previous := sema.vars
	sema.vars = maps.Clone(previous)
	defer func() { sema.vars = previous }()
	for name, value := range overrides {
		sema.SetContextType(name, value)
	}
	return check()
}
