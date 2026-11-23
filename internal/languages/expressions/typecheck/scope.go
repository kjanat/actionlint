package typecheck

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// Availability supplies name restrictions and overloads. ContextNames returns
// lowercase names.
type Availability interface {
	ContextNames() []string
	FunctionAvailability() map[string]functions.Availability
	FunctionOverloads() map[string][]*functions.FuncSignature
}

type Scope interface {
	Availability
	ContextTypes() map[string]exprtypes.Type
	// NarrowContext returns branch-specific type overrides and whether the branch
	// is possible. Insufficient knowledge means possible. Implementations must not
	// mutate syntax or types obtained through lookup.
	NarrowContext(guard syntax.Node, truth bool, lookup func(string) (exprtypes.Type, bool)) (map[string]exprtypes.Type, bool)
}

// SetAvailability replaces name restrictions and overloads without changing
// context types already inferred for the current expression.
func (sema *Checker) SetAvailability(scope Availability) {
	sema.SetContextAvailability(scope.ContextNames())
	sema.SetFunctionAvailability(scope.FunctionAvailability())
	sema.functionOverloads = make(map[string][]*functions.FuncSignature)
	for name, overloads := range scope.FunctionOverloads() {
		copied := make([]*functions.FuncSignature, len(overloads))
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
func (sema *Checker) SetFunctionAvailability(availability map[string]functions.Availability) {
	sema.functionAvailability = make(map[string]functions.Availability, len(availability))
	for name, entry := range availability {
		entry.AvailableAt = slices.Clone(entry.AvailableAt)
		sema.functionAvailability[strings.ToLower(name)] = entry
	}
}

func (sema *Checker) functionRestriction(callName string) (functions.Availability, bool) {
	availability, restricted := sema.functionAvailability[strings.ToLower(callName)]
	return availability, restricted && !availability.Allowed
}

// AvailableContexts returns permitted names and their current types. The caller
// owns the map; later scope updates do not change the returned entries.
func (sema *Checker) AvailableContexts() map[string]exprtypes.Type {
	contexts := make(map[string]exprtypes.Type, len(sema.availableContexts))
	for _, name := range sema.availableContexts {
		if ty, found := sema.vars[name]; found {
			contexts[name] = ty
		}
	}
	return contexts
}
