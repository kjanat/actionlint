package actionlint

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

func expressionTruthy(value any) bool {
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

// conditionConstantValue evaluates a conservative subset shared by condition
// diagnostics and invocation reachability. Unsupported functions, ambiguous
// JSON members and mode-dependent serialization remain unknown.
func conditionConstantValue(expr ExprNode) (any, bool) {
	switch n := expr.(type) {
	case *NullNode:
		return nil, true
	case *BoolNode:
		return n.Value, true
	case *IntNode:
		return float64(n.Value), true
	case *FloatNode:
		return n.Value, true
	case *StringNode:
		return n.Value, true
	case *NotOpNode:
		v, ok := conditionConstantValue(n.Operand)
		return !expressionTruthy(v), ok
	case *LogicalOpNode:
		left, ok := conditionConstantValue(n.Left)
		if !ok {
			return nil, false
		}
		if expressionTruthy(left) == (n.Kind == LogicalOpNodeKindOr) {
			return left, true
		}
		return conditionConstantValue(n.Right)
	case *CompareOpNode:
		left, lok := conditionConstantValue(n.Left)
		right, rok := conditionConstantValue(n.Right)
		if !lok || !rok {
			return nil, false
		}
		return compareConditionValues(n.Kind, left, right)
	case *FuncCallNode:
		if strings.EqualFold(n.Callee, "case") {
			if len(n.Args) < 3 || len(n.Args)%2 == 0 {
				return nil, false
			}
			for i := 0; i < len(n.Args)-1; i += 2 {
				value, known := conditionConstantValue(n.Args[i])
				predicate, boolean := value.(bool)
				if !known || !boolean {
					return nil, false
				}
				if predicate {
					return conditionConstantValue(n.Args[i+1])
				}
			}
			return conditionConstantValue(n.Args[len(n.Args)-1])
		}
		args := make([]any, len(n.Args))
		for i, arg := range n.Args {
			var ok bool
			if args[i], ok = conditionConstantValue(arg); !ok {
				return nil, false
			}
		}
		switch strings.ToLower(n.Callee) {
		case "join":
			if len(args) < 1 || len(args) > 2 {
				return nil, false
			}
			items, array := args[0].([]any)
			if !array {
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
				if s, ok := args[0].(string); ok && len(jsonMemberCollisions(s)) == 0 {
					var value any
					if json.Unmarshal([]byte(s), &value) == nil {
						return value, true
					}
				}
			}
		case "contains", "startswith", "endswith":
			if len(args) == 2 {
				if items, ok := args[0].([]any); ok && strings.EqualFold(n.Callee, "contains") {
					known := true
					for _, item := range items {
						equal, comparisonKnown := compareConditionValues(CompareOpNodeKindEq, item, args[1])
						if comparisonKnown && equal {
							return true, true
						}
						known = known && comparisonKnown
					}
					return false, known
				}
				left, lok := args[0].(string)
				right, rok := args[1].(string)
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
				if format, ok := args[0].(string); ok {
					return constantConditionFormat(format, args[1:])
				}
			}
		}
	}
	return nil, false
}

func compareConditionValues(kind CompareOpNodeKind, left, right any) (bool, bool) {
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
	return compareConditionNumbers(kind, matrixFilterNumber(left), matrixFilterNumber(right)), true
}

func constantJoinString(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case nil:
		return "", true
	case bool:
		return strconv.FormatBool(value), true
	case []any, map[string]any:
		return "", true
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
	default:
		return false
	}
}

func constantConditionFormat(format string, args []any) (any, bool) {
	var out strings.Builder
	for i := 0; i < len(format); {
		c := format[i]
		if c != '{' && c != '}' {
			out.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(format) && format[i+1] == c {
			out.WriteByte(c)
			i += 2
			continue
		}
		end := strings.IndexByte(format[i:], '}')
		if c != '{' || end < 2 {
			return nil, false
		}
		index, err := strconv.Atoi(format[i+1 : i+end])
		if err != nil || index < 0 || index >= len(args) {
			return nil, false
		}
		switch v := args[index].(type) {
		case string:
			out.WriteString(v)
		case nil:
		case bool:
			out.WriteString(strconv.FormatBool(v))
		default:
			// Numeric formatting differs across engines, including negative zero.
			return nil, false
		}
		i += end + 1
	}
	return out.String(), true
}
