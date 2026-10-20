package action

import (
	"fmt"
	"slices"
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
)

type actionFunctionLimits struct{ min, max int }

type actionExpressionAvailability struct {
	contexts  []string
	functions map[string]actionFunctionLimits
}

// Violation identifies an unavailable context, invalid call or expression syntax.
type Violation struct {
	Context string
	Message string
	// Offsets are one-based decoded byte positions; endOffset is exclusive.
	// Zero means the original position is unavailable after expression folding.
	Offset    int
	EndOffset int
}

// ExpressionViolations checks availability without inventing caller-owned types.
func ExpressionViolations(s string, bare bool, field string, inputs Inputs) []Violation {
	avail, ok := actionMetadataAvailability[field]
	if !ok {
		return nil
	}
	base, positionsKnown := 0, true
	if bare && (!strings.Contains(s, "${{") || exprlang.LiteralExpressionValue(s) != nil) {
		if _, err, _ := exprlang.ParseConditionExpression(s); err != nil {
			violation := Violation{Message: "has an invalid expression: " + err.Message}
			if exprlang.ConditionSource(s) == s {
				violation.Offset = err.Offset + 1
				violation.EndOffset = err.EndOffset + 1
			}
			return []Violation{violation}
		}
		folded := exprlang.ConditionSource(s)
		positionsKnown = folded == s
		s = "${{" + folded + "}}"
		base = -3 // The synthetic opening delimiter is absent from the scalar.
	}
	var out []Violation
	for {
		i := strings.Index(s, "${{")
		if i < 0 {
			break
		}
		s = s[i+3:]
		base += i + 3
		expressionOffset := base
		lex := exprlang.NewLexer(s)
		expr, err := exprlang.NewParser().Parse(lex)
		if err != nil {
			violation := Violation{Message: "has an invalid expression: " + err.Message}
			if positionsKnown {
				violation.Offset = expressionOffset + err.Offset + 1
				violation.EndOffset = expressionOffset + err.EndOffset + 1
			}
			out = append(out, violation)
			break
		}
		// The lexer consumes the closing delimiter and skips delimiters inside strings.
		s = s[lex.Offset():]
		base += lex.Offset()
		exprlang.Visit(expr, func(node, _ exprlang.Node, entering bool) {
			if !entering {
				return
			}
			var v Violation
			switch n := node.(type) {
			case *exprlang.VariableNode:
				if !slices.Contains(avail.contexts, strings.ToLower(n.Name)) {
					name := strings.ToLower(n.Name)
					var suggestion string
					if _, declared := inputs[name]; declared && slices.Contains(avail.contexts, "inputs") {
						suggestion = "inputs." + name
					} else {
						suggestion = exprlang.ContextPropertySuggestion(name, avail.contexts, exprlang.BuiltinGlobalVariableTypes)
					}
					if suggestion != "" {
						v.Message = fmt.Sprintf("uses undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
					} else {
						v.Context = name
					}
				}
			case *exprlang.FuncCallNode:
				name := strings.ToLower(n.Callee)
				if name == "fromjson" && len(n.Args) == 1 {
					if literal, ok := n.Args[0].(*exprlang.StringNode); ok {
						for _, collision := range exprlang.JSONMemberCollisions(literal.Value) {
							v := Violation{Message: fmt.Sprintf("fromJSON() object members %q and %q collide under case-insensitive lookup at JSON offset %d; the selected value depends on JSON parsing mode", collision.First, collision.Second, collision.Offset)}
							if positionsKnown {
								v.Offset = expressionOffset + n.Token().Offset + 1
								v.EndOffset = v.Offset + len(n.Token().Value)
							}
							out = append(out, v)
						}
					}
				}
				if !slices.Contains(actionMetadataSpecialFunctions, name) {
					signatures, ok := exprlang.BuiltinFuncSignatures[name]
					if !ok {
						v.Message = fmt.Sprintf("calls unknown function %q", name)
					} else if !actionFunctionArityMatches(signatures, len(n.Args)) ||
						(name == "format" || name == "case") && len(n.Args) > 255 ||
						name == "case" && len(n.Args)%2 == 0 {
						v.Message = fmt.Sprintf("calls function %q with an invalid number of arguments (%d)", name, len(n.Args))
					}
					break
				}
				bounds, ok := avail.functions[name]
				if !ok {
					v.Message = fmt.Sprintf("calls function %q which is not available here", name)
				} else if count := len(n.Args); count < bounds.min || bounds.max >= 0 && count > bounds.max {
					v.Message = fmt.Sprintf("calls function %q with %d argument(s); expected at least %d", name, count, bounds.min)
					if bounds.max >= 0 {
						v.Message += fmt.Sprintf(" and at most %d", bounds.max)
					}
				}
			}
			if v != (Violation{}) {
				if positionsKnown {
					v.Offset = expressionOffset + node.Token().Offset + 1
					v.EndOffset = v.Offset + len(node.Token().Value)
				}
				out = append(out, v)
			}
		})
	}
	if !positionsKnown {
		seen := make(map[Violation]bool)
		out = slices.DeleteFunc(out, func(v Violation) bool {
			if v.Offset != 0 {
				return false
			}
			duplicate := seen[v]
			seen[v] = true
			return duplicate
		})
	}
	return out
}

func actionFunctionArityMatches(signatures []*exprlang.FuncSignature, count int) bool {
	for _, signature := range signatures {
		if signature.VariableLengthParams && count >= len(signature.Params) || !signature.VariableLengthParams && count == len(signature.Params) {
			return true
		}
	}
	return false
}

// AllowedContexts returns the contexts available to an action field.
func AllowedContexts(field string) []string {
	return slices.Clone(actionMetadataAvailability[field].contexts)
}

// StepKeys returns the manifest keys accepted by a run-step or uses-step.
func StepKeys(kind string) []string { return slices.Clone(actionMetadataKeys[kind]) }
