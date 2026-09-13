package actionlint

import (
	"fmt"
	"slices"
	"strings"
)

type actionFunctionLimits struct{ min, max int }

type actionExpressionAvailability struct {
	contexts  []string
	functions map[string]actionFunctionLimits
}

type actionExpressionViolation struct {
	context string
	message string
}

// actionExpressionViolations checks availability without inventing types for caller-owned values.
func actionExpressionViolations(s string, bare bool, field string) []actionExpressionViolation {
	avail, ok := actionMetadataAvailability[field]
	if !ok {
		return nil
	}
	if bare && !strings.Contains(s, "${{") {
		s = "${{" + s + "}}"
	}
	seen := map[actionExpressionViolation]bool{}
	var out []actionExpressionViolation
	for {
		i := strings.Index(s, "${{")
		if i < 0 {
			break
		}
		s = s[i+3:]
		lex := NewExprLexer(s)
		expr, err := NewExprParser().Parse(lex)
		if err != nil || expr == nil {
			out = append(out, actionExpressionViolation{message: fmt.Sprintf("has an invalid expression: %v", err)})
			break
		}
		// The lexer consumes the closing delimiter and skips delimiters inside strings.
		s = s[lex.Offset():]
		VisitExprNode(expr, func(node, _ ExprNode, entering bool) {
			if !entering {
				return
			}
			var v actionExpressionViolation
			switch n := node.(type) {
			case *VariableNode:
				if !slices.Contains(avail.contexts, strings.ToLower(n.Name)) {
					v.context = strings.ToLower(n.Name)
				}
			case *FuncCallNode:
				name := strings.ToLower(n.Callee)
				if !slices.Contains(actionMetadataSpecialFunctions, name) {
					signatures, ok := BuiltinFuncSignatures[name]
					if !ok {
						v.message = fmt.Sprintf("calls unknown function %q", name)
					} else if !actionFunctionArityMatches(signatures, len(n.Args)) ||
						(name == "format" || name == "case") && len(n.Args) > 255 ||
						name == "case" && len(n.Args)%2 == 0 {
						v.message = fmt.Sprintf("calls function %q with an invalid number of arguments (%d)", name, len(n.Args))
					}
					break
				}
				bounds, ok := avail.functions[name]
				if !ok {
					v.message = fmt.Sprintf("calls function %q which is not available here", name)
				} else if count := len(n.Args); count < bounds.min || bounds.max >= 0 && count > bounds.max {
					v.message = fmt.Sprintf("calls function %q with %d argument(s); expected at least %d", name, count, bounds.min)
					if bounds.max >= 0 {
						v.message += fmt.Sprintf(" and at most %d", bounds.max)
					}
				}
			}
			if v != (actionExpressionViolation{}) && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		})
	}
	return out
}

func actionFunctionArityMatches(signatures []*FuncSignature, count int) bool {
	for _, signature := range signatures {
		if signature.VariableLengthParams && count >= len(signature.Params) || !signature.VariableLengthParams && count == len(signature.Params) {
			return true
		}
	}
	return false
}
