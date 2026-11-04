package expression

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

func ExpressionTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0 && !math.IsNaN(v)
	case string:
		return v != ""
	default:
		return true
	}
}

// ConditionConstantValue evaluates the subset used by condition diagnostics and
// invocation reachability. It reports unknown for unsupported functions,
// ambiguous JSON members and mode-dependent serialization.
func ConditionConstantValue(expr Node) (any, bool) {
	return ExpressionValue(expr, nil)
}

// ExpressionValue evaluates the same supported operations as constant folding,
// with a caller-supplied lookup for known context variables. A missing lookup or
// unknown variable stays unknown; absent properties of a known object are null.
// It never changes the parsed expression or the caller's context values.
func ExpressionValue(expr Node, variables func(string) (any, bool)) (any, bool) {
	return expressionValue(expr, variables, nil)
}

// ExpressionValueWithLookup also asks for qualified context paths, such as
// runner.os. This permits partial context knowledge without treating other
// properties as null. Unknown paths fall back to ordinary object evaluation.
func ExpressionValueWithLookup(expr Node, lookup func(string) (any, bool)) (any, bool) {
	return expressionValue(expr, lookup, lookup)
}

func expressionValue(expr Node, variables, properties func(string) (any, bool)) (any, bool) {
	if properties != nil {
		if reference := StaticReference(expr); strings.Contains(reference, ".") {
			if result, known := properties(reference); known {
				return result, true
			}
		}
	}
	switch n := expr.(type) {
	case *VariableNode:
		if variables == nil {
			return nil, false
		}
		return variables(n.Name)
	case *ObjectDerefNode:
		receiver, known := expressionValue(n.Receiver, variables, properties)
		if !known {
			return nil, false
		}
		return constantProperty(receiver, n.Property)
	case *IndexAccessNode:
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
	case *NullNode:
		return nil, true
	case *BoolNode:
		return n.Value, true
	case *IntNode:
		if n.Value == 0 && strings.HasPrefix(n.First.Value, "-") {
			return math.Copysign(0, -1), true
		}
		return float64(n.Value), true
	case *FloatNode:
		return n.Value, true
	case *StringNode:
		return n.Value, true
	case *NotOpNode:
		v, ok := expressionValue(n.Operand, variables, properties)
		return !ExpressionTruthy(v), ok
	case *LogicalOpNode:
		left, ok := expressionValue(n.Left, variables, properties)
		if !ok {
			return nil, false
		}
		if ExpressionTruthy(left) == (n.Kind == LogicalOpNodeKindOr) {
			return left, true
		}
		return expressionValue(n.Right, variables, properties)
	case *CompareOpNode:
		left, lok := expressionValue(n.Left, variables, properties)
		right, rok := expressionValue(n.Right, variables, properties)
		if !lok || !rok {
			return nil, false
		}
		return CompareConditionValues(n.Kind, left, right)
	case *FuncCallNode:
		signatures, found := BuiltinFuncSignatures[strings.ToLower(n.Callee)]
		if !found || len(signatures) == 0 {
			return nil, false
		}
		policy := signatures[0].EvaluationPolicy
		args := make([]any, len(n.Args))
		checked := make([]callArgument, len(n.Args))
		order := []int{}
		if len(n.Args) > 0 {
			order = append(order, 0)
		}
		for position := 0; position < len(order); position++ {
			i := order[position]
			demand := argumentDemand(policy, i, len(n.Args), checked)
			arg := callArgument{source: AnyType(), demand: demand}
			if demand != DemandSkipped {
				value, known := expressionValue(n.Args[i], variables, properties)
				if !known {
					return nil, false
				}
				arg.value, arg.state, arg.source = value, argumentKnown, TypeOfJSONValue(value)
				args[i] = value
			}
			checked[i] = arg
			if position == 0 {
				order = append(order, argumentExecutionOrder(policy, len(n.Args), checked)...)
			}
		}
		switch strings.ToLower(n.Callee) {
		case "case":
			if len(args) < 3 || len(args)%2 == 0 {
				return nil, false
			}
			for i := 0; i < len(args)-1; i += 2 {
				if checked[i].demand == DemandSkipped {
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
				return constantJoinString(args[0])
			}
			separator := ","
			if len(args) == 2 && len(items) > 1 {
				switch args[1].(type) {
				case []any, map[string]any:
					// Non-primitive separators leave the default comma unchanged.
				default:
					var ok bool
					separator, ok = constantJoinString(args[1])
					if !ok {
						return nil, false
					}
				}
			}
			parts := make([]string, len(items))
			for i, item := range items {
				var ok bool
				parts[i], ok = constantJoinString(item)
				if !ok {
					return nil, false
				}
			}
			return strings.Join(parts, separator), true
		case "fromjson":
			if len(args) == 1 {
				if s, ok := constantFunctionString(args[0]); ok {
					parsed, err := parseFromJSON(s)
					// Loose JSON erases integer -0's sign; strict JSON can keep
					// it. Do not turn mode-dependent formatting into a constant.
					if err == nil && len(parsed.collisions) == 0 && !parsed.integerNegativeZero {
						return parsed.value, true
					}
				}
			}
		case "contains", "startswith", "endswith":
			if len(args) == 2 {
				if items, ok := args[0].([]any); ok && strings.EqualFold(n.Callee, "contains") {
					known := true
					for _, item := range items {
						equal, comparisonKnown := CompareConditionValues(CompareOpNodeKindEq, item, args[1])
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
				left, lok := constantPredicateString(args[0])
				right, rok := constantPredicateString(args[1])
				if lok && rok {
					left, right = ordinalIgnoreCaseKey(left), ordinalIgnoreCaseKey(right)
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
				if plan, known := argumentFormatPlan(&checked[0], len(args)-1); known {
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
	name = ordinalIgnoreCaseKey(name)
	var value any
	found := false
	for key, item := range object {
		if ordinalIgnoreCaseKey(key) != name {
			continue
		}
		if found {
			return nil, false
		}
		value, found = item, true
	}
	return value, true
}

func constantPredicateString(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case nil:
		return "", true
	case bool:
		return strconv.FormatBool(value), true
	case float64:
		// These integers have the same decimal representation under the
		// runner's invariant G15 format and round-trip number formatting.
		if math.Abs(value) < 1e15 && math.Trunc(value) == value && (value != 0 || !math.Signbit(value)) {
			return strconv.FormatFloat(value, 'f', 0, 64), true
		}
	}
	return "", false
}

func CompareConditionValues(kind CompareOpNodeKind, left, right any) (bool, bool) {
	if l, ok := left.(string); ok {
		if r, ok := right.(string); ok {
			order := slices.Compare(utf16.Encode([]rune(ordinalIgnoreCaseKey(l))), utf16.Encode([]rune(ordinalIgnoreCaseKey(r))))
			return compareConditionNumbers(kind, float64(order), 0), true
		}
	}
	composite := func(value any) bool {
		switch value.(type) {
		case map[string]any, []any:
			return true
		default:
			return false
		}
	}
	// Two composite values can require identity tracking. Against a primitive,
	// their numeric coercion is NaN and the comparison has a known result.
	if composite(left) && composite(right) {
		return false, false
	}
	return compareConditionNumbers(kind, ToNumber(left), ToNumber(right)), true
}

func constantFunctionString(value any) (string, bool) {
	if s, ok := constantPredicateString(value); ok {
		return s, true
	}
	switch value.(type) {
	case []any, map[string]any:
		return constantJoinString(value)
	default:
		return "", false
	}
}

func constantJoinString(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case nil:
		return "", true
	case bool:
		return strconv.FormatBool(value), true
	case []any:
		return "Array", true
	case map[string]any:
		return "Object", true
	default:
		// Do not assume an engine's numeric serialization, including signed zero.
		return "", false
	}
}

func compareConditionNumbers(kind CompareOpNodeKind, a, b float64) bool {
	switch kind {
	case CompareOpNodeKindEq:
		return a == b
	case CompareOpNodeKindNotEq:
		return a != b
	case CompareOpNodeKindLess:
		return a < b
	case CompareOpNodeKindLessEq:
		return a <= b
	case CompareOpNodeKindGreater:
		return a > b
	case CompareOpNodeKindGreaterEq:
		return a >= b
	case CompareOpNodeKindInvalid:
		// An invalid operator never holds.
		fallthrough
	default:
		return false
	}
}

func constantPlannedFormat(plan formatPlan, args []any) (any, bool) {
	if !plan.valid {
		return nil, false
	}
	var out strings.Builder
	for _, part := range plan.parts {
		if part.index < 0 {
			out.WriteString(part.text)
			continue
		}
		if part.specifier != "" {
			return nil, false
		}
		value, known := constantFunctionString(args[part.index])
		if !known {
			return nil, false
		}
		out.WriteString(value)
	}
	return out.String(), true
}
