package evaluate

import (
	"math"
	"strings"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
)

// ConditionConstantValue evaluates the subset used by condition diagnostics and
// invocation reachability. It reports unknown for unsupported functions,
// ambiguous JSON members and mode-dependent serialization.
func ConditionConstantValue(expr exprsyntax.Node) (any, bool) {
	return ExpressionValue(expr, nil)
}

// ExpressionValue evaluates the same supported operations as constant folding,
// with a caller-supplied lookup for known context variables. A missing lookup or
// unknown variable stays unknown; absent properties of a known object are null.
// It never changes the parsed expression or the caller's context values.
func ExpressionValue(expr exprsyntax.Node, variables func(string) (any, bool)) (any, bool) {
	return expressionValue(expr, variables, nil)
}

// ExpressionValueWithLookup also asks for qualified context paths, such as
// runner.os. This permits partial context knowledge without treating other
// properties as null. Unknown paths fall back to ordinary object evaluation.
func ExpressionValueWithLookup(expr exprsyntax.Node, lookup func(string) (any, bool)) (any, bool) {
	return expressionValue(expr, lookup, lookup)
}

func expressionValue(expr exprsyntax.Node, variables, properties func(string) (any, bool)) (any, bool) {
	if properties != nil {
		if reference := exprsyntax.StaticReference(expr); strings.Contains(reference, ".") {
			if result, known := properties(reference); known {
				return result, true
			}
		}
	}
	switch n := expr.(type) {
	case *exprsyntax.VariableNode:
		if variables == nil {
			return nil, false
		}
		return variables(n.Name)
	case *exprsyntax.ObjectDerefNode:
		receiver, known := expressionValue(n.Receiver, variables, properties)
		if !known {
			return nil, false
		}
		return constantProperty(receiver, n.Property)
	case *exprsyntax.IndexAccessNode:
		receiver, known := expressionValue(n.Operand, variables, properties)
		if !known {
			return nil, false
		}
		index, known := expressionValue(n.Index, variables, properties)
		if !known {
			return nil, false
		}
		if name, ok := index.(string); ok {
			switch receiver.(type) {
			case nil, map[string]any:
				return constantProperty(receiver, name)
			default:
				return nil, false
			}
		}
		if items, ok := receiver.([]any); ok {
			if index, ok := index.(float64); ok && !math.IsNaN(index) && !math.IsInf(index, 0) && math.Trunc(index) == index {
				if index < 0 || index >= float64(len(items)) {
					return nil, true
				}
				return items[int(index)], true
			}
		}
		return nil, false
	case *exprsyntax.NullNode:
		return nil, true
	case *exprsyntax.BoolNode:
		return n.Value, true
	case *exprsyntax.IntNode:
		if n.Value == 0 && strings.HasPrefix(n.First.Value, "-") {
			return math.Copysign(0, -1), true
		}
		return float64(n.Value), true
	case *exprsyntax.FloatNode:
		return n.Value, true
	case *exprsyntax.StringNode:
		return n.Value, true
	case *exprsyntax.NotOpNode:
		v, ok := expressionValue(n.Operand, variables, properties)
		return !exprvalues.ExpressionTruthy(v), ok
	case *exprsyntax.LogicalOpNode:
		left, ok := expressionValue(n.Left, variables, properties)
		if !ok {
			return nil, false
		}
		if exprvalues.ExpressionTruthy(left) == (n.Kind == exprsyntax.LogicalOpNodeKindOr) {
			return left, true
		}
		return expressionValue(n.Right, variables, properties)
	case *exprsyntax.CompareOpNode:
		left, lok := expressionValue(n.Left, variables, properties)
		right, rok := expressionValue(n.Right, variables, properties)
		if !lok || !rok {
			return nil, false
		}
		return exprvalues.CompareConditionValues(n.Kind, left, right)
	case *exprsyntax.FuncCallNode:
		function, found := exprfunctions.Builtins[strings.ToLower(n.Callee)]
		if !found {
			return nil, false
		}
		context := &functionContext{call: n, variables: variables, properties: properties, results: make([]exprvalues.EvaluationResult, len(n.Args)), observed: make([]bool, len(n.Args))}
		result := function.Evaluate(context)
		return result.Value, result.State == exprvalues.EvaluationKnown
	}
	return nil, false
}

func constantProperty(receiver any, name string) (any, bool) {
	object, ok := receiver.(map[string]any)
	if !ok {
		return nil, true
	}
	name = runnerjson.OrdinalIgnoreCaseKey(name)
	var value any
	found := false
	for key, item := range object {
		if runnerjson.OrdinalIgnoreCaseKey(key) != name {
			continue
		}
		if found {
			return nil, false
		}
		value, found = item, true
	}
	return value, true
}

// functionContext caches source arguments while implementations choose which to request.
type functionContext struct {
	call       *exprsyntax.FuncCallNode
	variables  func(string) (any, bool)
	properties func(string) (any, bool)
	results    []exprvalues.EvaluationResult
	observed   []bool
}

func (context *functionContext) ArgumentCount() int { return len(context.call.Args) }
func (context *functionContext) Argument(index int) exprvalues.EvaluationResult {
	if !context.observed[index] {
		value, known := expressionValue(context.call.Args[index], context.variables, context.properties)
		result := exprvalues.EvaluationResult{State: exprvalues.EvaluationUnknown}
		if known {
			result = exprvalues.EvaluationResult{State: exprvalues.EvaluationKnown, Value: value}
		}
		context.results[index] = result
		context.observed[index] = true
	}
	return context.results[index]
}
