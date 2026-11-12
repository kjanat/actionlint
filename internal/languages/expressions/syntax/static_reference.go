package syntax

import "strings"

// StaticReference returns a context path for static dot or literal bracket access.
func StaticReference(expr Node) string {
	path, _ := ReferencePath(expr)
	return strings.Join(path, ".")
}

// ReferencePath returns case-insensitive property names for a static reference.
// Literal keys containing dots occupy one segment.
func ReferencePath(expr Node) ([]string, bool) {
	switch n := expr.(type) {
	case *VariableNode:
		return []string{strings.ToLower(n.Name)}, true
	case *ObjectDerefNode:
		if receiver, ok := ReferencePath(n.Receiver); ok {
			return append(receiver, strings.ToLower(n.Property)), true
		}
	case *IndexAccessNode:
		if key, ok := n.Index.(*StringNode); ok {
			if receiver, ok := ReferencePath(n.Operand); ok {
				return append(receiver, strings.ToLower(key.Value)), true
			}
		}
	}
	return nil, false
}
