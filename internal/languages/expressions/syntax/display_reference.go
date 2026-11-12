package syntax

// DisplayReference formats a reference for diagnostics using its source spelling.
// Dynamic indexes appear as [] and array projections as .* .
func DisplayReference(n Node) string {
	switch n := n.(type) {
	case *VariableNode:
		if n.First.Value != "" {
			return n.First.Value
		}
		return n.Name
	case *ArrayDerefNode:
		if base := DisplayReference(n.Receiver); base != "" {
			return base + ".*"
		}
	case *IndexAccessNode:
		if base := DisplayReference(n.Operand); base != "" {
			if lit, ok := n.Index.(*StringNode); ok {
				return base + "." + lit.Value
			}
			return base + "[]"
		}
	case *ObjectDerefNode:
		if base := DisplayReference(n.Receiver); base != "" {
			return base + "." + n.PropertyName()
		}
	}
	return ""
}
