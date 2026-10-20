package expression

import "strings"

// CheckSecretInterpolations reports secret values used to generate script text.
// Quotes in the script are not a boundary: interpolation precedes shell parsing.
// Conditions and predicates are safe because they do not emit the secret value.
func CheckSecretInterpolations(expr Node) []*Error {
	var findings []*Error
	var visit func(Node)
	visit = func(node Node) {
		if secretValue(node) {
			name := eventReference(node)
			if name == "" {
				name = "secrets"
			}
			findings = append(findings, errorfAtExpr(findingKind{"script-injection", "secret-interpolation"}, node,
				"secret %q is interpolated directly into script text; pass it through an environment variable and read that variable as data instead", name))
			return
		}
		switch n := node.(type) {
		case *LogicalOpNode:
			if value, known := ConditionConstantValue(n.Left); known {
				if ExpressionTruthy(value) != (n.Kind == LogicalOpNodeKindOr) {
					visit(n.Right)
				}
				return
			}
			if n.Kind == LogicalOpNodeKindOr {
				visit(n.Left)
			}
			visit(n.Right)
		case *FuncCallNode:
			if isSafeFuncCall(n) || strings.EqualFold(n.Callee, "hashFiles") {
				return
			}
			if strings.EqualFold(n.Callee, "case") && len(n.Args) >= 3 && len(n.Args)%2 == 1 {
				for i := 0; i < len(n.Args)-1; i += 2 {
					if value, known := ConditionConstantValue(n.Args[i]); known {
						if !ExpressionTruthy(value) {
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
		case *ObjectDerefNode:
			visit(n.Receiver)
		case *ArrayDerefNode:
			visit(n.Receiver)
		case *IndexAccessNode:
			// The index selects a value; its own contents are not serialized.
			visit(n.Operand)
		}
	}
	visit(expr)
	return findings
}

func secretValue(node Node) bool {
	switch n := node.(type) {
	case *VariableNode:
		return n.Name == "secrets"
	case *ObjectDerefNode:
		if root, ok := n.Receiver.(*VariableNode); ok && root.Name == "github" && n.Property == "token" {
			return true
		}
		return secretValue(n.Receiver)
	case *ArrayDerefNode:
		return secretValue(n.Receiver)
	case *IndexAccessNode:
		if root, ok := n.Operand.(*VariableNode); ok && root.Name == "github" {
			if key, ok := n.Index.(*StringNode); ok && strings.EqualFold(key.Value, "token") {
				return true
			}
		}
		return secretValue(n.Operand)
	default:
		return false
	}
}
