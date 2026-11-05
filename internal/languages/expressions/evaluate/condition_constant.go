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
	result := Evaluate(expr, variables)
	return result.Value, result.State == exprvalues.EvaluationKnown
}

// Evaluate reports a known value, missing runtime knowledge, a runtime failure,
// or a call rejected before evaluation. It does not perform name or type checks.
func Evaluate(expr exprsyntax.Node, variables func(string) (any, bool)) exprvalues.EvaluationResult {
	return expressionResult(expr, variables, nil)
}

// ExpressionValueWithLookup also asks for qualified context paths, such as
// runner.os. This permits partial context knowledge without treating other
// properties as null. Unknown paths fall back to ordinary object evaluation.
func ExpressionValueWithLookup(expr exprsyntax.Node, lookup func(string) (any, bool)) (any, bool) {
	result := EvaluateWithLookup(expr, lookup)
	return result.Value, result.State == exprvalues.EvaluationKnown
}

// EvaluateWithLookup accepts known qualified paths as well as root variables.
func EvaluateWithLookup(expr exprsyntax.Node, lookup func(string) (any, bool)) exprvalues.EvaluationResult {
	return expressionResult(expr, lookup, lookup)
}

func knownResult(value any) exprvalues.EvaluationResult {
	return exprvalues.EvaluationResult{State: exprvalues.EvaluationKnown, Value: value}
}

func unknownResult() exprvalues.EvaluationResult {
	return exprvalues.EvaluationResult{State: exprvalues.EvaluationUnknown}
}

func observedResult(value any, known bool) exprvalues.EvaluationResult {
	if known {
		return knownResult(value)
	}
	return unknownResult()
}

func expressionResult(expr exprsyntax.Node, variables, properties func(string) (any, bool)) exprvalues.EvaluationResult {
	if properties != nil {
		if reference := exprsyntax.StaticReference(expr); strings.Contains(reference, ".") {
			if result, known := properties(reference); known {
				return knownResult(result)
			}
		}
	}
	switch n := expr.(type) {
	case *exprsyntax.VariableNode:
		if variables == nil {
			return unknownResult()
		}
		return observedResult(variables(n.Name))
	case *exprsyntax.ObjectDerefNode:
		receiver := expressionResult(n.Receiver, variables, properties)
		if receiver.State != exprvalues.EvaluationKnown {
			return receiver
		}
		return observedResult(constantProperty(receiver.Value, n.Property))
	case *exprsyntax.IndexAccessNode:
		receiver := expressionResult(n.Operand, variables, properties)
		if receiver.State != exprvalues.EvaluationKnown {
			return receiver
		}
		index := expressionResult(n.Index, variables, properties)
		if index.State != exprvalues.EvaluationKnown {
			return index
		}
		if name, ok := index.Value.(string); ok {
			switch receiver.Value.(type) {
			case nil, map[string]any:
				return observedResult(constantProperty(receiver.Value, name))
			default:
				return unknownResult()
			}
		}
		if items, ok := receiver.Value.([]any); ok {
			if index, ok := index.Value.(float64); ok && !math.IsNaN(index) && !math.IsInf(index, 0) && math.Trunc(index) == index {
				if index < 0 || index >= float64(len(items)) {
					return knownResult(nil)
				}
				return knownResult(items[int(index)])
			}
		}
		return unknownResult()
	case *exprsyntax.NullNode:
		return knownResult(nil)
	case *exprsyntax.BoolNode:
		return knownResult(n.Value)
	case *exprsyntax.IntNode:
		if n.Value == 0 && strings.HasPrefix(n.First.Value, "-") {
			return knownResult(math.Copysign(0, -1))
		}
		return knownResult(float64(n.Value))
	case *exprsyntax.FloatNode:
		return knownResult(n.Value)
	case *exprsyntax.StringNode:
		return knownResult(n.Value)
	case *exprsyntax.NotOpNode:
		value := expressionResult(n.Operand, variables, properties)
		if value.State != exprvalues.EvaluationKnown {
			return value
		}
		return knownResult(!exprvalues.ExpressionTruthy(value.Value))
	case *exprsyntax.LogicalOpNode:
		left := expressionResult(n.Left, variables, properties)
		if left.State != exprvalues.EvaluationKnown {
			return left
		}
		if exprvalues.ExpressionTruthy(left.Value) == (n.Kind == exprsyntax.LogicalOpNodeKindOr) {
			return left
		}
		return expressionResult(n.Right, variables, properties)
	case *exprsyntax.CompareOpNode:
		left := expressionResult(n.Left, variables, properties)
		if left.State != exprvalues.EvaluationKnown {
			return left
		}
		right := expressionResult(n.Right, variables, properties)
		if right.State != exprvalues.EvaluationKnown {
			return right
		}
		return observedResult(exprvalues.CompareConditionValues(n.Kind, left.Value, right.Value))
	case *exprsyntax.FuncCallNode:
		function, found := exprfunctions.Builtins[strings.ToLower(n.Callee)]
		if !found {
			return unknownResult()
		}
		context := &functionContext{call: n, variables: variables, properties: properties, results: make([]exprvalues.EvaluationResult, len(n.Args)), observed: make([]bool, len(n.Args))}
		return function.Evaluate(context)
	}
	return unknownResult()
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
		context.results[index] = expressionResult(context.call.Args[index], context.variables, context.properties)
		context.observed[index] = true
	}
	return context.results[index]
}
