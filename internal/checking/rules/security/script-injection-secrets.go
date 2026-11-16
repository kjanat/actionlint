package security

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// checkSecretInterpolations reports secret values used to generate script text.
// Quotes in the script are not a boundary: interpolation precedes shell parsing.
// Conditions and predicates are safe because they do not emit the secret value.
func checkSecretInterpolations(expr syntax.Node, semanticErrors []*typecheck.Error) []*typecheck.Error {
	var findings []*typecheck.Error
	var visit func(syntax.Node)
	visit = func(node syntax.Node) {
		if secretValue(node) {
			name := syntax.DisplayReference(node)
			if name == "" {
				name = "secrets"
			}
			span := syntax.ExpressionSpan(node)
			invalid := slices.ContainsFunc(semanticErrors, func(issue *typecheck.Error) bool {
				return issue.Offset < span.End && span.Start < issue.EndOffset
			})
			if !invalid {
				findings = append(findings, &typecheck.Error{
					Rule: "script-injection", Code: "secret-interpolation",
					Message: fmt.Sprintf("secret %q is interpolated directly into script text; pass it through an environment variable and read that variable as data instead", name),
					Offset:  span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column,
				})
			}
			return
		}
		switch n := node.(type) {
		case *syntax.LogicalOpNode:
			if value, known := evaluate.ConditionConstantValue(n.Left); known {
				if values.ExpressionTruthy(value) != (n.Kind == syntax.LogicalOpNodeKindOr) {
					visit(n.Right)
				}
				return
			}
			if n.Kind == syntax.LogicalOpNodeKindOr {
				visit(n.Left)
			}
			visit(n.Right)
		case *syntax.FuncCallNode:
			if functions.IsStringPredicate(n.Callee) || strings.EqualFold(n.Callee, "hashFiles") {
				return
			}
			if strings.EqualFold(n.Callee, "case") && len(n.Args) >= 3 && len(n.Args)%2 == 1 {
				for i := 0; i < len(n.Args)-1; i += 2 {
					if value, known := evaluate.ConditionConstantValue(n.Args[i]); known {
						if !values.ExpressionTruthy(value) {
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
		case *syntax.ObjectDerefNode:
			visit(n.Receiver)
		case *syntax.ArrayDerefNode:
			visit(n.Receiver)
		case *syntax.IndexAccessNode:
			// The index selects a value; its own contents are not serialized.
			visit(n.Operand)
		}
	}
	visit(expr)
	return findings
}

func secretValue(node syntax.Node) bool {
	switch n := node.(type) {
	case *syntax.VariableNode:
		return n.Name == "secrets"
	case *syntax.ObjectDerefNode:
		if root, ok := n.Receiver.(*syntax.VariableNode); ok && strings.EqualFold(root.Name, "github") && strings.EqualFold(n.Property, "token") {
			return true
		}
		return secretValue(n.Receiver)
	case *syntax.ArrayDerefNode:
		return secretValue(n.Receiver)
	case *syntax.IndexAccessNode:
		if root, ok := n.Operand.(*syntax.VariableNode); ok && strings.EqualFold(root.Name, "github") {
			if value, known := evaluate.ConditionConstantValue(n.Index); known {
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
