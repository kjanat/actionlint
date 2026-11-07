package syntax

import "strings"

// StaticReference returns a context path for static dot or literal bracket access.
func StaticReference(expr Node) string {
	switch n := expr.(type) {
	case *VariableNode:
		return strings.ToLower(n.Name)
	case *ObjectDerefNode:
		if receiver := StaticReference(n.Receiver); receiver != "" {
			return receiver + "." + strings.ToLower(n.Property)
		}
	case *IndexAccessNode:
		if key, ok := n.Index.(*StringNode); ok {
			if receiver := StaticReference(n.Operand); receiver != "" {
				return receiver + "." + strings.ToLower(key.Value)
			}
		}
	}
	return ""
}
