package evaluate

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

const (
	maxStringAlternatives = 32
	maxAlternativeBytes   = 64 * 1024
	maxAlternativeDepth   = 64
)

// StringAlternatives returns a complete, bounded set of string results for
// constants, case() branches and && / || choices. Unknown predicates select
// among known outputs; an unknown output, invalid predicate or exceeded budget
// makes the whole set unknown. It never substitutes a predicate's contents into
// a result.
func StringAlternatives(expr syntax.Node) ([]string, bool) {
	values, complete := valueAlternatives(expr, 0)
	if !complete {
		return nil, false
	}
	results := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		results = append(results, text)
	}
	return results, true
}

func valueAlternatives(expr syntax.Node, depth int) ([]any, bool) {
	if depth > maxAlternativeDepth {
		return nil, false
	}
	if value, known := ConditionConstantValue(expr); known {
		switch value := value.(type) {
		case string:
			if len(value) > maxAlternativeBytes {
				return nil, false
			}
		case nil, bool, float64:
		default:
			return nil, false
		}
		return []any{value}, true
	}
	switch n := expr.(type) {
	case *syntax.CompareOpNode, *syntax.NotOpNode:
		return []any{true, false}, true
	case *syntax.LogicalOpNode:
		return logicalAlternatives(n, depth)
	case *syntax.FuncCallNode:
		if booleanFunction(n.Callee) {
			return []any{true, false}, true
		}
		if strings.EqualFold(n.Callee, "case") {
			return caseAlternatives(n, depth)
		}
	}
	return nil, false
}

func booleanFunction(name string) bool {
	if functions.IsStringPredicate(name) {
		return true
	}
	switch strings.ToLower(name) {
	case "success", "failure", "cancelled", "always":
		return true
	}
	return false
}

// logicalAlternatives keeps the left results that && or || returns as they
// are and adds the right operand's results when the left can pass control on.
func logicalAlternatives(n *syntax.LogicalOpNode, depth int) ([]any, bool) {
	if n.Kind != syntax.LogicalOpNodeKindAnd && n.Kind != syntax.LogicalOpNodeKindOr {
		return nil, false
	}
	left, complete := valueAlternatives(n.Left, depth+1)
	if !complete {
		return nil, false
	}
	returnsLeft := n.Kind == syntax.LogicalOpNodeKindOr
	var results []any
	continues := false
	for _, value := range left {
		if exprvalues.ExpressionTruthy(value) != returnsLeft {
			continues = true
			continue
		}
		if results, complete = unionValues(results, []any{value}); !complete {
			return nil, false
		}
	}
	if !continues {
		return results, true
	}
	right, complete := valueAlternatives(n.Right, depth+1)
	if !complete {
		return nil, false
	}
	return unionValues(results, right)
}

func caseAlternatives(call *syntax.FuncCallNode, depth int) ([]any, bool) {
	if len(call.Args) < 3 || len(call.Args)%2 != 1 {
		return nil, false
	}
	var results []any
	for index := 0; index < len(call.Args)-1; index += 2 {
		predicate, known := ConditionConstantValue(call.Args[index])
		if known {
			condition, boolean := predicate.(bool)
			if !boolean {
				return nil, false
			}
			if !condition {
				continue
			}
		}
		branch, complete := valueAlternatives(call.Args[index+1], depth+1)
		if !complete {
			return nil, false
		}
		results, complete = unionValues(results, branch)
		if !complete {
			return nil, false
		}
		if known {
			return results, true
		}
	}
	other, complete := valueAlternatives(call.Args[len(call.Args)-1], depth+1)
	if !complete {
		return nil, false
	}
	return unionValues(results, other)
}

func unionValues(first, second []any) ([]any, bool) {
	for _, value := range second {
		if !slices.Contains(first, value) {
			if len(first) == maxStringAlternatives {
				return nil, false
			}
			first = append(first, value)
		}
	}
	return first, true
}

func unionStrings(first, second []string) ([]string, bool) {
	for _, text := range second {
		if !slices.Contains(first, text) {
			if len(first) == maxStringAlternatives {
				return nil, false
			}
			first = append(first, text)
		}
	}
	return first, true
}

// TemplateStringAlternatives preserves literal template pieces and combines
// complete expression result sets. Evaluated text is data: it is never parsed
// again as an expression, including text containing expression delimiters.
func TemplateStringAlternatives(source string) ([]string, bool) {
	results := []string{""}
	for {
		index := strings.Index(source, "${{")
		if index < 0 {
			return concatenateStrings(results, []string{source})
		}
		var complete bool
		results, complete = concatenateStrings(results, []string{source[:index]})
		if !complete {
			return nil, false
		}
		source = source[index+3:]
		lexer := syntax.NewLexer(source)
		expr, err := syntax.NewParser().ParseExpression(lexer)
		if err != nil {
			return nil, false
		}
		end := lexer.Offset()
		if end < 2 || end > len(source) || source[end-2:end] != "}}" {
			return nil, false
		}
		values, complete := StringAlternatives(expr)
		if !complete {
			return nil, false
		}
		results, complete = concatenateStrings(results, values)
		if !complete {
			return nil, false
		}
		source = source[end:]
	}
}

func concatenateStrings(first, second []string) ([]string, bool) {
	var results []string
	for _, prefix := range first {
		for _, suffix := range second {
			if len(prefix)+len(suffix) > maxAlternativeBytes {
				return nil, false
			}
			var complete bool
			results, complete = unionStrings(results, []string{prefix + suffix})
			if !complete {
				return nil, false
			}
		}
	}
	return results, true
}
