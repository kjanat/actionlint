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
	// Offsets are one-based decoded byte positions; endOffset is exclusive.
	// Zero means the original position is unavailable after expression folding.
	offset    int
	endOffset int
}

// actionExpressionViolations checks availability without inventing types for caller-owned values.
func actionExpressionViolations(s string, bare bool, field string) []actionExpressionViolation {
	return actionExpressionViolationsWithInputs(s, bare, field, nil)
}

func actionExpressionViolationsWithInputs(s string, bare bool, field string, inputs ActionMetadataInputs) []actionExpressionViolation {
	avail, ok := actionMetadataAvailability[field]
	if !ok {
		return nil
	}
	base, positionsKnown := 0, true
	if bare && (!strings.Contains(s, "${{") || literalExpressionValue(s) != nil) {
		if _, err, _ := parseConditionExpression(s); err != nil {
			violation := actionExpressionViolation{message: "has an invalid expression: " + err.Message}
			if conditionSource(s) == s {
				violation.offset = err.Offset + 1
				violation.endOffset = err.EndOffset + 1
			}
			return []actionExpressionViolation{violation}
		}
		folded := conditionSource(s)
		positionsKnown = folded == s
		s = "${{" + folded + "}}"
		base = -3 // The synthetic opening delimiter is absent from the scalar.
	}
	var out []actionExpressionViolation
	for {
		i := strings.Index(s, "${{")
		if i < 0 {
			break
		}
		s = s[i+3:]
		base += i + 3
		expressionOffset := base
		lex := NewExprLexer(s)
		expr, err := NewExprParser().Parse(lex)
		if err != nil {
			violation := actionExpressionViolation{message: "has an invalid expression: " + err.Message}
			if positionsKnown {
				violation.offset = expressionOffset + err.Offset + 1
				violation.endOffset = expressionOffset + err.EndOffset + 1
			}
			out = append(out, violation)
			break
		}
		// The lexer consumes the closing delimiter and skips delimiters inside strings.
		s = s[lex.Offset():]
		base += lex.Offset()
		VisitExprNode(expr, func(node, _ ExprNode, entering bool) {
			if !entering {
				return
			}
			var v actionExpressionViolation
			switch n := node.(type) {
			case *VariableNode:
				if !slices.Contains(avail.contexts, strings.ToLower(n.Name)) {
					name := strings.ToLower(n.Name)
					var suggestion string
					if _, declared := inputs[name]; declared && slices.Contains(avail.contexts, "inputs") {
						suggestion = "inputs." + name
					} else {
						suggestion = contextPropertySuggestion(name, avail.contexts, BuiltinGlobalVariableTypes)
					}
					if suggestion != "" {
						v.message = fmt.Sprintf("uses undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
					} else {
						v.context = name
					}
				}
			case *FuncCallNode:
				name := strings.ToLower(n.Callee)
				if name == "fromjson" && len(n.Args) == 1 {
					if literal, ok := n.Args[0].(*StringNode); ok {
						for _, collision := range jsonMemberCollisions(literal.Value) {
							v := actionExpressionViolation{message: fmt.Sprintf("fromJSON() object members %q and %q collide under case-insensitive lookup at JSON offset %d; the selected value depends on JSON parsing mode", collision.first, collision.second, collision.offset)}
							if positionsKnown {
								v.offset = expressionOffset + n.Token().Offset + 1
								v.endOffset = v.offset + len(n.Token().Value)
							}
							out = append(out, v)
						}
					}
				}
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
			if v != (actionExpressionViolation{}) {
				if positionsKnown {
					v.offset = expressionOffset + node.Token().Offset + 1
					v.endOffset = v.offset + len(node.Token().Value)
				}
				out = append(out, v)
			}
		})
	}
	if !positionsKnown {
		seen := make(map[actionExpressionViolation]bool)
		out = slices.DeleteFunc(out, func(v actionExpressionViolation) bool {
			if v.offset != 0 {
				return false
			}
			duplicate := seen[v]
			seen[v] = true
			return duplicate
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
