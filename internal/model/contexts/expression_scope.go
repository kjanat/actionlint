package contexts

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

type ExpressionScope struct{ key string }

func (scope ExpressionScope) NarrowContext(guard syntax.Node, truth bool, lookup func(string) (exprtypes.Type, bool)) (map[string]exprtypes.Type, bool) {
	return NarrowEventTypes(guard, truth, lookup)
}

// DefaultExpressionScope enables no context references or location-restricted
// functions. Select field availability before checking references.
func DefaultExpressionScope() ExpressionScope { return ExpressionScope{} }

func WorkflowExpressionScope(key string) ExpressionScope { return ExpressionScope{key: key} }

func (scope ExpressionScope) ContextTypes() map[string]exprtypes.Type {
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
			Name: name, Ret: exprtypes.BoolType(), Params: []exprtypes.Type{exprtypes.StringType()}, VariableLengthParams: true,
		})
	}
	return overloads
}
