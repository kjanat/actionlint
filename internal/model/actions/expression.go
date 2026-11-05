package action

import (
	"fmt"
	"slices"
	"strings"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

type actionFunctionLimits struct{ min, max int }

type actionExpressionAvailability struct {
	contexts  []string
	functions map[string]actionFunctionLimits
}

// ExpressionProblem identifies invalid expression syntax, unavailable names or suspicious coercion.
type ExpressionProblem struct {
	Code    ProblemCode
	Context string
	Message string
	// Offsets are one-based decoded byte positions; endOffset is exclusive.
	// Zero means the original position is unavailable after expression folding.
	Offset    int
	EndOffset int
}

// ValidateExpression checks syntax and availability without assuming invocation-specific types.
func ValidateExpression(s string, bare bool, field string, inputs Inputs) []ExpressionProblem {
	return expressionViolations(s, bare, field, inputs, false)
}

// InspectExpression also identifies suspicious coercions reported by the shared checker.
func InspectExpression(s string, bare bool, field string, inputs Inputs) []ExpressionProblem {
	return expressionViolations(s, bare, field, inputs, true)
}

func expressionViolations(s string, bare bool, field string, inputs Inputs, advisories bool) []ExpressionProblem {
	avail, ok := actionMetadataAvailability[field]
	if !ok {
		return nil
	}
	base, positionsKnown := 0, true
	if bare && (!strings.Contains(s, "${{") || exprsyntax.LiteralExpressionValue(s) != nil) {
		if _, err, _ := exprsyntax.ParseConditionExpression(s); err != nil {
			violation := ExpressionProblem{Code: CodeExpressionSyntax, Message: "has an invalid expression: " + err.Message}
			if exprsyntax.ConditionSource(s) == s {
				violation.Offset = err.Offset + 1
				violation.EndOffset = err.EndOffset + 1
			}
			return []ExpressionProblem{violation}
		}
		folded := exprsyntax.ConditionSource(s)
		positionsKnown = folded == s
		s = "${{" + folded + "}}"
		base = -3 // The synthetic opening delimiter is absent from the scalar.
	}
	var out []ExpressionProblem
	for {
		i := strings.Index(s, "${{")
		if i < 0 {
			break
		}
		s = s[i+3:]
		base += i + 3
		expressionOffset := base
		lex := exprsyntax.NewLexer(s)
		expr, err := exprsyntax.NewParser().Parse(lex)
		if err != nil {
			violation := ExpressionProblem{Code: CodeExpressionSyntax, Message: "has an invalid expression: " + err.Message}
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
		if advisories {
			checker := exprlang.NewChecker(&exprlang.Options{Checks: exprlang.Checks{SuspiciousCoercion: true}})
			checker.SetContextAvailability(avail.contexts)
			checker.UpdateInputs(exprtypes.NewMapObjectType(exprtypes.StringType()))
			checker.UpdateSteps(exprtypes.NewMapObjectType(exprtypes.AnyType()))
			result := checker.CheckResult(expr)
			for _, finding := range result.Findings {
				if !finding.Advisory || finding.Code != string(CodeCoercion) {
					continue
				}
				v := ExpressionProblem{Code: CodeCoercion, Message: finding.Message}
				if positionsKnown {
					v.Offset, v.EndOffset = expressionOffset+finding.Offset+1, expressionOffset+finding.EndOffset+1
				}
				out = append(out, v)
			}
		}
		exprsyntax.Visit(expr, func(node, _ exprsyntax.Node, entering bool) {
			if !entering {
				return
			}
			var v ExpressionProblem
			switch n := node.(type) {
			case *exprsyntax.VariableNode:
				if !slices.Contains(avail.contexts, strings.ToLower(n.Name)) {
					name := strings.ToLower(n.Name)
					var suggestion string
					if _, declared := inputs[name]; declared && slices.Contains(avail.contexts, "inputs") {
						suggestion = "inputs." + name
					} else {
						suggestion = exprlang.ContextPropertySuggestion(name, avail.contexts, exprlang.BuiltinGlobalVariableTypes)
					}
					if suggestion == "" {
						suggestion = exprlang.CloseName(name, avail.contexts)
					}
					if suggestion != "" {
						v.Code = CodeExpressionReference
						v.Message = fmt.Sprintf("uses undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
					} else {
						v.Code = CodeExpressionContext
						v.Context = name
					}
				}
			case *exprsyntax.FuncCallNode:
				name := strings.ToLower(n.Callee)
				if name == "fromjson" && len(n.Args) == 1 {
					if literal, ok := n.Args[0].(*exprsyntax.StringNode); ok {
						for _, collision := range runnerjson.JSONMemberCollisions(literal.Value) {
							v := ExpressionProblem{Code: CodeJSONMemberCollision, Message: fmt.Sprintf("fromJSON() object members %q and %q collide under case-insensitive lookup at JSON offset %d; the selected value depends on JSON parsing mode", collision.First, collision.Second, collision.Offset)}
							if positionsKnown {
								v.Offset = expressionOffset + n.Token().Offset + 1
								v.EndOffset = v.Offset + len(n.Token().Value)
							}
							out = append(out, v)
						}
					}
				}
				if !slices.Contains(actionMetadataSpecialFunctions, name) {
					signatures, ok := exprfunctions.BuiltinFuncSignatures[name]
					if !ok {
						v.Code = CodeExpressionCall
						v.Message = fmt.Sprintf("calls unknown function %q", name)
						var candidates []string
						for candidate, signatures := range exprfunctions.BuiltinFuncSignatures {
							if slices.Contains(actionMetadataSpecialFunctions, candidate) {
								if _, available := avail.functions[candidate]; !available {
									continue
								}
							}
							if len(signatures) > 0 {
								candidates = append(candidates, signatures[0].Name)
							}
						}
						if suggestion := exprlang.CloseName(name, candidates); suggestion != "" {
							v.Message += fmt.Sprintf("; did you mean %q?", suggestion)
						}
					} else if !actionFunctionArityMatches(signatures, len(n.Args)) ||
						(name == "format" || name == "case") && len(n.Args) > 255 ||
						name == "case" && len(n.Args)%2 == 0 {
						v.Code = CodeExpressionCall
						v.Message = fmt.Sprintf("calls function %q with an invalid number of arguments (%d)", name, len(n.Args))
					}
					break
				}
				bounds, ok := avail.functions[name]
				if !ok {
					v.Code = CodeExpressionCall
					v.Message = fmt.Sprintf("calls function %q which is not available here", name)
				} else if count := len(n.Args); count < bounds.min || bounds.max >= 0 && count > bounds.max {
					v.Code = CodeExpressionCall
					v.Message = fmt.Sprintf("calls function %q with %d argument(s); expected at least %d", name, count, bounds.min)
					if bounds.max >= 0 {
						v.Message += fmt.Sprintf(" and at most %d", bounds.max)
					}
				}
			}
			if v != (ExpressionProblem{}) {
				if positionsKnown {
					v.Offset = expressionOffset + node.Token().Offset + 1
					v.EndOffset = v.Offset + len(node.Token().Value)
				}
				out = append(out, v)
			}
		})
	}
	if !positionsKnown {
		seen := make(map[ExpressionProblem]bool)
		out = slices.DeleteFunc(out, func(v ExpressionProblem) bool {
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

func actionFunctionArityMatches(signatures []*exprfunctions.FuncSignature, count int) bool {
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
