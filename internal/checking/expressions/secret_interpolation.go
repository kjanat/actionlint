package expressions

import (
	"fmt"
	"slices"
	"strings"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// checkSecretInterpolations reports secret values used to generate script text.
// Quotes in the script are not a boundary: interpolation precedes shell parsing.
// Conditions and predicates are safe because they do not emit the secret value.
func checkSecretInterpolations(expr exprsyntax.Node, semanticErrors []*exprlang.Error) []*exprlang.Error {
	var findings []*exprlang.Error
	var visit func(exprsyntax.Node)
	visit = func(node exprsyntax.Node) {
		if secretValue(node) {
			name := exprsyntax.DisplayReference(node)
			if name == "" {
				name = "secrets"
			}
			span := exprsyntax.ExpressionSpan(node)
			invalid := slices.ContainsFunc(semanticErrors, func(issue *exprlang.Error) bool {
				return issue.Offset < span.End && span.Start < issue.EndOffset
			})
			if !invalid {
				findings = append(findings, &exprlang.Error{
					Rule: "script-injection", Code: "secret-interpolation",
					Message: fmt.Sprintf("secret %q is interpolated directly into script text; pass it through an environment variable and read that variable as data instead", name),
					Offset:  span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column,
				})
			}
			return
		}
		switch n := node.(type) {
		case *exprsyntax.LogicalOpNode:
			if value, known := expreval.ConditionConstantValue(n.Left); known {
				if exprvalues.ExpressionTruthy(value) != (n.Kind == exprsyntax.LogicalOpNodeKindOr) {
					visit(n.Right)
				}
				return
			}
			if n.Kind == exprsyntax.LogicalOpNodeKindOr {
				visit(n.Left)
			}
			visit(n.Right)
		case *exprsyntax.FuncCallNode:
			if exprfunctions.IsStringPredicate(n.Callee) || strings.EqualFold(n.Callee, "hashFiles") {
				return
			}
			if strings.EqualFold(n.Callee, "case") && len(n.Args) >= 3 && len(n.Args)%2 == 1 {
				for i := 0; i < len(n.Args)-1; i += 2 {
					if value, known := expreval.ConditionConstantValue(n.Args[i]); known {
						if !exprvalues.ExpressionTruthy(value) {
							continue
						}
						visit(n.Args[i+1])
						return
					}
					visit(n.Args[i+1])
				}
				visit(n.Args[len(n.Args)-1])
				return
			}
			for _, arg := range n.Args {
				visit(arg)
			}
		case *exprsyntax.ObjectDerefNode:
			visit(n.Receiver)
		case *exprsyntax.ArrayDerefNode:
			visit(n.Receiver)
		case *exprsyntax.IndexAccessNode:
			// The index selects a value; its own contents are not serialized.
			visit(n.Operand)
		}
	}
	visit(expr)
	return findings
}

func secretValue(node exprsyntax.Node) bool {
	switch n := node.(type) {
	case *exprsyntax.VariableNode:
		return n.Name == "secrets"
	case *exprsyntax.ObjectDerefNode:
		if root, ok := n.Receiver.(*exprsyntax.VariableNode); ok && strings.EqualFold(root.Name, "github") && strings.EqualFold(n.Property, "token") {
			return true
		}
		return secretValue(n.Receiver)
	case *exprsyntax.ArrayDerefNode:
		return secretValue(n.Receiver)
	case *exprsyntax.IndexAccessNode:
		if root, ok := n.Operand.(*exprsyntax.VariableNode); ok && strings.EqualFold(root.Name, "github") {
			if value, known := expreval.ConditionConstantValue(n.Index); known {
				if key, ok := value.(string); ok && strings.EqualFold(key, "token") {
					return true
				}
			}
		}
		return secretValue(n.Operand)
	default:
		return false
	}
}
