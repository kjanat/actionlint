package context

import (
	"maps"
	"slices"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// ExpressionScope supplies GitHub context types and the availability for one
// workflow field. Invocation-specific types can replace its initial types later.
type ExpressionScope struct{ key string }

// DefaultExpressionScope supplies context types with no field restriction and
// marks location-restricted functions unavailable.
func DefaultExpressionScope() ExpressionScope { return ExpressionScope{} }

// WorkflowExpressionScope selects the names and overloads for a workflow field.
func WorkflowExpressionScope(key string) ExpressionScope { return ExpressionScope{key: key} }

func (scope ExpressionScope) ContextTypes() map[string]exprtypes.Type {
	return maps.Clone(BuiltinGlobalVariableTypes)
}

func (scope ExpressionScope) ContextNames() []string {
	contexts, _ := WorkflowKeyAvailability(scope.key)
	return slices.Clone(contexts)
}

func (scope ExpressionScope) FunctionAvailability() map[string]exprfunctions.Availability {
	_, allowed := WorkflowKeyAvailability(scope.key)
	return FunctionAvailability(allowed)
}

// FunctionAvailability resolves the generated location table against an explicit
// list of permitted function names.
func FunctionAvailability(allowed []string) map[string]exprfunctions.Availability {
	availability := make(map[string]exprfunctions.Availability, len(SpecialFunctionNames))
	for name, locations := range SpecialFunctionNames {
		availability[name] = exprfunctions.Availability{Allowed: slices.Contains(allowed, name), AvailableAt: slices.Clone(locations)}
	}
	return availability
}

func (scope ExpressionScope) FunctionOverloads() map[string][]*exprfunctions.FuncSignature {
	if scope.key != "jobs.<job_id>.if" {
		return nil
	}
	overloads := make(map[string][]*exprfunctions.FuncSignature, 2)
	for _, name := range []string{"success", "failure"} {
		overloads[name] = append(slices.Clone(exprfunctions.Builtins[name].Signatures), &exprfunctions.FuncSignature{
			Name: name, Ret: exprtypes.BoolType(), Params: []exprtypes.Type{exprtypes.StringType()}, VariableLengthParams: true,
		})
	}
	return overloads
}
