package typecheck

import (
	"slices"
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

const (
	maxStringAlternatives = 32
	maxAlternativeBytes   = 64 * 1024
	maxAlternativeDepth   = 64
)

// StringAlternatives returns a complete, bounded set of string results for
// constants and case() branches. Unknown predicates select among known outputs;
// an unknown output, invalid predicate or exceeded budget makes the whole set
// unknown. It never substitutes a predicate's contents into a result.
func StringAlternatives(expr exprsyntax.Node) ([]string, bool) {
	return stringAlternatives(expr, 0)
}

func stringAlternatives(expr exprsyntax.Node, depth int) ([]string, bool) {
	if depth > maxAlternativeDepth {
		return nil, false
	}
	if value, known := expreval.ConditionConstantValue(expr); known {
		text, ok := value.(string)
		if !ok || len(text) > maxAlternativeBytes {
			return nil, false
		}
		return []string{text}, true
	}
	call, ok := expr.(*exprsyntax.FuncCallNode)
	if !ok || !strings.EqualFold(call.Callee, "case") || len(call.Args) < 3 || len(call.Args)%2 != 1 {
		return nil, false
	}
	var results []string
	for index := 0; index < len(call.Args)-1; index += 2 {
		predicate, known := expreval.ConditionConstantValue(call.Args[index])
		if known {
			condition, boolean := predicate.(bool)
			if !boolean {
				return nil, false
			}
			if !condition {
				continue
			}
		}
		branch, complete := stringAlternatives(call.Args[index+1], depth+1)
		if !complete {
			return nil, false
		}
		results, complete = unionStrings(results, branch)
		if !complete {
			return nil, false
		}
		if known {
			return results, true
		}
	}
	other, complete := stringAlternatives(call.Args[len(call.Args)-1], depth+1)
	if !complete {
		return nil, false
	}
	return unionStrings(results, other)
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
		lexer := exprsyntax.NewLexer(source)
		expr, err := exprsyntax.NewParser().Parse(lexer)
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
