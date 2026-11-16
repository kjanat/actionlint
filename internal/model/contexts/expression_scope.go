package contexts

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// ExpressionScope supplies GitHub context types and the availability for one
// workflow field. Invocation-specific types can replace its initial types later.
type ExpressionScope struct{ key string }

func (scope ExpressionScope) NarrowContext(guard syntax.Node, truth bool, lookup func(string) (types.Type, bool)) (map[string]types.Type, bool) {
	return NarrowEventTypes(guard, truth, lookup)
}

// DefaultExpressionScope supplies initial types without enabling context
// references or location-restricted functions. Select field availability before
// checking references.
func DefaultExpressionScope() ExpressionScope { return ExpressionScope{} }

// WorkflowExpressionScope selects the names and overloads for a workflow field.
func WorkflowExpressionScope(key string) ExpressionScope { return ExpressionScope{key: key} }

func (scope ExpressionScope) ContextTypes() map[string]types.Type {
	return maps.Clone(BuiltinGlobalVariableTypes)
}

func (scope ExpressionScope) ContextNames() []string {
	contexts, _ := WorkflowKeyAvailability(scope.key)
	return slices.Clone(contexts)
}

func (scope ExpressionScope) FunctionAvailability() map[string]functions.Availability {
	_, allowed := WorkflowKeyAvailability(scope.key)
	return FunctionAvailability(allowed)
}

// FunctionAvailability resolves the generated location table against an explicit
// list of permitted function names.
func FunctionAvailability(allowed []string) map[string]functions.Availability {
	availability := make(map[string]functions.Availability, len(SpecialFunctionNames))
	for name, locations := range SpecialFunctionNames {
		availability[name] = functions.Availability{Allowed: slices.Contains(allowed, name), AvailableAt: slices.Clone(locations)}
	}
	return availability
}

func (scope ExpressionScope) FunctionOverloads() map[string][]*functions.FuncSignature {
	if scope.key != "jobs.<job_id>.if" {
		return nil
	}
	overloads := make(map[string][]*functions.FuncSignature, 2)
	for _, name := range []string{"success", "failure"} {
		overloads[name] = append(slices.Clone(functions.Builtins[name].Signatures), &functions.FuncSignature{
			Name: name, Ret: types.BoolType(), Params: []types.Type{types.StringType()}, VariableLengthParams: true,
		})
	}
	return overloads
}
