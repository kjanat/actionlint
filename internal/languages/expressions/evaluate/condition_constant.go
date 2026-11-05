package evaluate

import (
	"math"
	"strconv"
	"strings"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

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
		signatures, found := exprfunctions.BuiltinFuncSignatures[strings.ToLower(n.Callee)]
		if !found || len(signatures) == 0 {
			return nil, false
		}
		policy := signatures[0].EvaluationPolicy
		args := make([]any, len(n.Args))
		checked := make([]exprfunctions.CallArgument, len(n.Args))
		order := []int{}
		if len(n.Args) > 0 {
			order = append(order, 0)
		}
		for position := 0; position < len(order); position++ {
			i := order[position]
			demand := exprfunctions.DemandForArgument(policy, i, len(n.Args), checked)
			arg := exprfunctions.CallArgument{Source: exprtypes.AnyType(), Demand: demand}
			if demand != exprfunctions.DemandSkipped {
				value, known := expressionValue(n.Args[i], variables, properties)
				if !known {
					return nil, false
				}
				arg.Value, arg.State, arg.Source = value, exprfunctions.ArgumentKnown, exprtypes.TypeOfJSONValue(value)
				args[i] = value
			}
			checked[i] = arg
			if position == 0 {
				order = append(order, exprfunctions.ArgumentExecutionOrder(policy, len(n.Args), checked)...)
			}
		}
		switch strings.ToLower(n.Callee) {
		case "case":
			if len(args) < 3 || len(args)%2 == 0 {
				return nil, false
			}
			for i := 0; i < len(args)-1; i += 2 {
				if checked[i].Demand == exprfunctions.DemandSkipped {
					continue
				}
				predicate, boolean := args[i].(bool)
				if !boolean {
					return nil, false
				}
				if predicate {
					return args[i+1], true
				}
			}
			return args[len(args)-1], true
		case "tojson":
			if len(args) == 1 {
				switch value := args[0].(type) {
				case nil:
					return "null", true
				case bool:
					return strconv.FormatBool(value), true
				case string:
					// Only fold strings that need no escaping in either JSON engine.
					for _, r := range value {
						if r < ' ' || r > '~' || strings.ContainsRune("\"\\<>&'+`", r) {
							return nil, false
						}
					}
					return `"` + value + `"`, true
				}
			}
		case "join":
			if len(args) < 1 || len(args) > 2 {
				return nil, false
			}
			items, array := args[0].([]any)
			if !array {
				if _, object := args[0].(map[string]any); object {
					return "", true
				}
				return exprvalues.JoinString(args[0])
			}
			separator := ","
			if len(args) == 2 && len(items) > 1 {
				switch args[1].(type) {
				case []any, map[string]any:
					// Non-primitive separators leave the default comma unchanged.
				default:
					var ok bool
					separator, ok = exprvalues.JoinString(args[1])
					if !ok {
						return nil, false
					}
				}
			}
			parts := make([]string, len(items))
			for i, item := range items {
				var ok bool
				parts[i], ok = exprvalues.JoinString(item)
				if !ok {
					return nil, false
				}
			}
			return strings.Join(parts, separator), true
		case "fromjson":
			if len(args) == 1 {
				if s, ok := exprvalues.FunctionString(args[0]); ok {
					parsed, err := runnerjson.Parse(s)
					// Loose JSON erases integer -0's sign; strict JSON can keep
					// it. Do not turn mode-dependent formatting into a constant.
					if err == nil && len(parsed.Collisions) == 0 && !parsed.IntegerNegativeZero {
						return parsed.Value, true
					}
				}
			}
		case "contains", "startswith", "endswith":
			if len(args) == 2 {
				if items, ok := args[0].([]any); ok && strings.EqualFold(n.Callee, "contains") {
					known := true
					for _, item := range items {
						equal, comparisonKnown := exprvalues.CompareConditionValues(exprsyntax.CompareOpNodeKindEq, item, args[1])
						if comparisonKnown && equal {
							return true, true
						}
						known = known && comparisonKnown
					}
					return false, known
				}
				// Composite arguments are accepted, but the runner returns false
				// without converting them to strings.
				if _, ok := args[0].([]any); ok {
					return false, true
				}
				if _, ok := args[0].(map[string]any); ok {
					return false, true
				}
				if _, ok := args[1].([]any); ok {
					return false, true
				}
				if _, ok := args[1].(map[string]any); ok {
					return false, true
				}
				left, lok := exprvalues.PredicateString(args[0])
				right, rok := exprvalues.PredicateString(args[1])
				if lok && rok {
					left, right = runnerjson.OrdinalIgnoreCaseKey(left), runnerjson.OrdinalIgnoreCaseKey(right)
					switch strings.ToLower(n.Callee) {
					case "contains":
						return strings.Contains(left, right), true
					case "startswith":
						return strings.HasPrefix(left, right), true
					default:
						return strings.HasSuffix(left, right), true
					}
				}
			}
		case "format":
			if len(args) > 0 {
				if plan, known := exprfunctions.ArgumentFormatPlan(&checked[0], len(args)-1); known {
					return constantPlannedFormat(plan, args[1:])
				}
			}
		}
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

func constantPlannedFormat(plan exprfunctions.FormatPlan, args []any) (any, bool) {
	if !plan.Valid() {
		return nil, false
	}
	var out strings.Builder
	for part := range plan.Parts() {
		if part.Index < 0 {
			out.WriteString(part.Text)
			continue
		}
		if part.Specifier != "" {
			return nil, false
		}
		value, known := exprvalues.FunctionString(args[part.Index])
		if !known {
			return nil, false
		}
		out.WriteString(value)
	}
	return out.String(), true
}
