package typecheck

import (
	"slices"
	"strings"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// Availability supplies the name restrictions and overloads for an expression.
// Its provider decides which source location or execution context they describe.
// ContextNames supplies lowercase names.
type Availability interface {
	ContextNames() []string
	FunctionAvailability() map[string]exprfunctions.Availability
	FunctionOverloads() map[string][]*exprfunctions.FuncSignature
}

// Scope supplies the initial variable types as well as name availability.
type Scope interface {
	Availability
	ContextTypes() map[string]exprtypes.Type
	// NarrowContext returns branch-specific type overrides and whether the branch
	// is possible. Insufficient knowledge means possible. Implementations must not
	// mutate syntax or types obtained through lookup.
	NarrowContext(guard exprsyntax.Node, truth bool, lookup func(string) (exprtypes.Type, bool)) (map[string]exprtypes.Type, bool)
}

// SetAvailability replaces name restrictions and overloads without changing
// context types already inferred for the current expression.
func (sema *Checker) SetAvailability(scope Availability) {
	sema.SetContextAvailability(scope.ContextNames())
	sema.SetFunctionAvailability(scope.FunctionAvailability())
	sema.functionOverloads = make(map[string][]*exprfunctions.FuncSignature)
	for name, overloads := range scope.FunctionOverloads() {
		copied := make([]*exprfunctions.FuncSignature, len(overloads))
		for index, signature := range overloads {
			value := *signature
			value.Params = slices.Clone(signature.Params)
			copied[index] = &value
		}
		sema.functionOverloads[strings.ToLower(name)] = copied
	}
}

// SetFunctionAvailability replaces the supplied restrictions. Functions absent
// from the map have no location restriction.
func (sema *Checker) SetFunctionAvailability(availability map[string]exprfunctions.Availability) {
	sema.functionAvailability = make(map[string]exprfunctions.Availability, len(availability))
	for name, entry := range availability {
		entry.AvailableAt = slices.Clone(entry.AvailableAt)
		sema.functionAvailability[strings.ToLower(name)] = entry
	}
}

func (sema *Checker) functionRestriction(callName string) (exprfunctions.Availability, bool) {
	availability, restricted := sema.functionAvailability[strings.ToLower(callName)]
	return availability, restricted && !availability.Allowed
}
